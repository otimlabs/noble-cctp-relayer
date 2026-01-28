package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	cctptypes "github.com/circlefin/noble-cctp/x/cctp/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/gin-gonic/gin"
	"github.com/mr-tron/base58"
	"github.com/spf13/cobra"

	"cosmossdk.io/log"
	"cosmossdk.io/math"

	"github.com/strangelove-ventures/noble-cctp-relayer/circle"
	"github.com/strangelove-ventures/noble-cctp-relayer/ethereum"
	"github.com/strangelove-ventures/noble-cctp-relayer/noble"
	"github.com/strangelove-ventures/noble-cctp-relayer/relayer"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// State and Store map the iris api lookup id -> MessageState
// State represents all in progress burns/mints
// Store represents terminal states
var State = types.NewStateMap()

// SequenceMap maps the domain -> the equivalent minter account sequence or nonce
var sequenceMap = types.NewSequenceMap()

func Start(a *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start relaying CCTP transactions",

		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			a.InitAppState()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := a.Logger
			cfg := a.Config

			flushInterval, err := cmd.Flags().GetDuration(flagFlushInterval)
			if err != nil {
				logger.Error("Invalid flush interval", "error", err)
			}

			flushOnly, err := cmd.Flags().GetBool(flagFlushOnlyMode)
			if err != nil {
				return fmt.Errorf("invalid flush only flag error=%w", err)
			}

			if flushInterval == 0 {
				if flushOnly {
					return fmt.Errorf("flush only mode requires a flush interval")
				} else {
					logger.Error("Flush interval not set. Use the --flush-interval flag to set a reoccurring flush")
				}
			}

			// start API on normal relayer only
			go startAPI(a)

			// messageState processing queue
			var processingQueue = make(chan *types.TxState, 10000)

			registeredDomains := make(map[types.Domain]types.Chain)

			port, err := cmd.Flags().GetInt16(flagMetricsPort)
			if err != nil {
				return fmt.Errorf("invalid port error=%w", err)
			}

			address, err := cmd.Flags().GetString(flagMetricsAddress)
			if err != nil {
				return fmt.Errorf("invalid address error=%w", err)
			}

			metrics := relayer.InitPromMetrics(address, port)

			for name, cfg := range cfg.Chains {
				c, err := cfg.Chain(name)
				if err != nil {
					return fmt.Errorf("error creating chain error=%w", err)
				}

				logger = logger.With("name", c.Name(), "domain", c.Domain())

				if err := c.InitializeClients(cmd.Context(), logger); err != nil {
					return fmt.Errorf("error initializing client error=%w", err)
				}

				go c.TrackLatestBlockHeight(cmd.Context(), logger, metrics)

				// wait until height is available
				maxRetries := 45
				for i := 0; i < maxRetries; i++ {
					if c.LatestBlock() == 0 {
						time.Sleep(1 * time.Second)
					} else {
						break
					}
					if i == maxRetries-1 {
						return fmt.Errorf("unable to get height")
					}
				}

				if err := c.InitializeBroadcaster(cmd.Context(), logger, sequenceMap); err != nil {
					return fmt.Errorf("error initializing broadcaster error=%w", err)
				}

				go c.StartListener(cmd.Context(), logger, processingQueue, flushOnly, flushInterval)

				go c.WalletBalanceMetric(cmd.Context(), a.Logger, metrics)

				if _, ok := registeredDomains[c.Domain()]; ok {
					return fmt.Errorf("duplicate domain found domain=%d name=%s", c.Domain(), c.Name())
				}

				registeredDomains[c.Domain()] = c
			}

			// Start Fast Transfer allowance monitor (v2 only)
			var domains []types.Domain
			for domain := range registeredDomains {
				domains = append(domains, domain)
			}
			circle.StartAllowanceMonitor(cmd.Context(), cfg.Circle, logger, domains, metrics)

			// spin up Processor worker pool
			for i := 0; i < int(cfg.ProcessorWorkerCount); i++ {
				go StartProcessor(cmd.Context(), a, registeredDomains, processingQueue, sequenceMap, metrics)
			}

			// wait for context to be done
			<-cmd.Context().Done()

			// close clients & output latest block heights
			for _, c := range registeredDomains {
				logger.Info(fmt.Sprintf("%s: latest-block: %d last-flushed-block: %d", c.Name(), c.LatestBlock(), c.LastFlushedBlock()))
				err := c.CloseClients()
				if err != nil {
					logger.Error("Error closing clients", "error", err)
				}
			}

			return nil
		},
	}

	return cmd
}

// StartProcessor is the main processing pipeline.
func StartProcessor(
	ctx context.Context,
	a *AppState,
	registeredDomains map[types.Domain]types.Chain,
	processingQueue chan *types.TxState,
	sequenceMap *types.SequenceMap,
	metrics *relayer.PromMetrics,
) {
	logger := a.Logger
	cfg := a.Config

	for {
		dequeuedTx := <-processingQueue

		// if this is the first time seeing this message, add it to the State
		tx, ok := State.Load(dequeuedTx.TxHash)
		if !ok {
			State.Store(dequeuedTx.TxHash, dequeuedTx)
			tx, _ = State.Load(dequeuedTx.TxHash)
			for _, msg := range tx.Msgs {
				msg.Status = types.Created
			}
		}

		var broadcastMsgs = make(map[types.Domain][]*types.MessageState)
		var requeue bool

		apiVersion, apiErr := cfg.Circle.GetAPIVersion()
		if apiErr != nil {
			logger.Debug("Failed to get API version", "error", apiErr)
		}

		for _, msg := range tx.Msgs {
			// if a filter's condition is met, mark as filtered
			if FilterDisabledCCTPRoutes(cfg, logger, msg) ||
				filterInvalidDestinationCallers(registeredDomains, logger, msg) ||
				filterLowTransfers(cfg, logger, msg) ||
				filterNonWhitelistedMintRecipients(cfg, logger, msg) {
				State.Mu.Lock()
				msg.Status = types.Filtered
				State.Mu.Unlock()
			}

			// if the message is burned or pending, check for an attestation
			if msg.Status == types.Created || msg.Status == types.Pending {
				response := circle.CheckAttestation(cfg.Circle, logger, msg.IrisLookupID, msg.SourceTxHash, msg.SourceDomain, msg.DestDomain)

				switch {
				case response == nil:
					logger.Debug("Attestation is still processing for 0x" + msg.IrisLookupID + ".  Retrying...")
					requeue = true
					continue
				case msg.Status == types.Created && response.Status == "pending_confirmations":
					logger.Debug("Attestation is created but still pending confirmations for 0x" + msg.IrisLookupID + ".  Retrying...")
					State.Mu.Lock()
					msg.Status = types.Pending
					msg.Updated = time.Now()
					State.Mu.Unlock()
					requeue = true
					continue
				case response.Status == "pending_confirmations":
					logger.Debug("Attestation is still pending for 0x" + msg.IrisLookupID + ".  Retrying...")
					requeue = true
					continue
				case response.Status == "complete":
					logger.Debug("Attestation is complete for 0x" + msg.IrisLookupID + ".")

					// Update state under lock
					State.Mu.Lock()
					msg.Status = types.Attested
					msg.Attestation = response.Attestation
					msg.Updated = time.Now()
					State.Mu.Unlock()

					// Fetch message details for Fast Transfer expiration tracking
					if apiVersion == types.APIVersionV2 {
						msgResp, err := circle.GetAttestationV2Message(
							cfg.Circle.AttestationBaseURL, logger, msg.SourceTxHash, msg.SourceDomain)
						if err != nil {
							logger.Debug("Failed to fetch v2 message details", "error", err, "txHash", msg.SourceTxHash)
						} else if msgResp != nil {
							State.Mu.Lock()
							msg.CctpVersion = msgResp.CctpVersion
							msg.ExpirationBlock = circle.ParseExpirationBlock(msgResp.ExpirationBlock)
							State.Mu.Unlock()
						}
					}

					broadcastMsgs[msg.DestDomain] = append(broadcastMsgs[msg.DestDomain], msg)
				default:
					logger.Error("Attestation failed for unknown reason for 0x" + msg.IrisLookupID + ".  Status: " + response.Status)
				}
			}

			// Handle expired Fast Transfer attestations (v2 only)
			if apiVersion == types.APIVersionV2 && msg.Status == types.Attested && msg.ExpirationBlock > 0 {
				if destChain, ok := registeredDomains[msg.DestDomain]; ok {
					result, err := circle.HandleExpiringAttestation(msg, cfg.Circle, destChain.LatestBlock(), logger)
					if err != nil {
						logger.Error("Re-attestation handling failed", "nonce", msg.Nonce, "error", err)
					}

					circle.ApplyReattestResult(State, msg, result)

					if result.RemoveFromQueue {
						circle.RemoveMessageFromQueue(broadcastMsgs, msg)
						requeue = true
						continue
					}

					if result.ExhaustedRetries {
						continue
					}
				}
			}
		}

		// if the message is attested to, try to broadcast
		for domain, msgs := range broadcastMsgs {
			chain, ok := registeredDomains[domain]
			if !ok {
				logger.Error("No chain registered for domain", "domain", domain)
				continue
			}

			if err := chain.Broadcast(ctx, logger, msgs, sequenceMap, metrics); err != nil {
				logger.Error("Unable to mint one or more transfers", "error(s)", err, "total_transfers", len(msgs), "name", chain.Name(), "domain", domain)
				requeue = true
				continue
			}

			State.Mu.Lock()
			for _, msg := range msgs {
				msg.Status = types.Complete
				msg.Updated = time.Now()
			}
			State.Mu.Unlock()
		}

		// requeue txs, ensure not to exceed retry limit
		if requeue {
			if dequeuedTx.RetryAttempt < cfg.Circle.FetchRetries {
				dequeuedTx.RetryAttempt++
				time.Sleep(time.Duration(cfg.Circle.FetchRetryInterval) * time.Second)
				processingQueue <- tx
			} else {
				logger.Error("Retry limit exceeded for tx", "limit", cfg.Circle.FetchRetries, "tx", dequeuedTx.TxHash)
			}
		}
	}
}

// filterDisabledCCTPRoutes returns true if we haven't enabled relaying from a source domain to a destination domain
func FilterDisabledCCTPRoutes(cfg *types.Config, logger log.Logger, msg *types.MessageState) bool {
	val, ok := cfg.EnabledRoutes[msg.SourceDomain]
	if !ok {
		logger.Info(fmt.Sprintf("Filtered tx %s because relaying from %d to %d is not enabled",
			msg.SourceTxHash, msg.SourceDomain, msg.DestDomain))
		return !ok
	}
	for _, dd := range val {
		if dd == msg.DestDomain {
			return false
		}
	}
	logger.Info(fmt.Sprintf("Filtered tx %s because relaying from %d to %d is not enabled",
		msg.SourceTxHash, msg.SourceDomain, msg.DestDomain))
	return true
}

// filterInvalidDestinationCallers returns true if the minter is not the destination caller for the specified domain
func filterInvalidDestinationCallers(registeredDomains map[types.Domain]types.Chain, logger log.Logger, msg *types.MessageState) bool {
	chain, ok := registeredDomains[msg.DestDomain]
	if !ok {
		logger.Error("No chain registered for domain", "domain", msg.DestDomain)
		return true
	}
	validCaller, address := chain.IsDestinationCaller(msg.DestinationCaller)

	if validCaller {
		// we do not want to filter this message if valid caller
		return false
	}

	logger.Info(fmt.Sprintf("Filtered tx %s from %d to %d due to destination caller: %s)",
		msg.SourceTxHash, msg.SourceDomain, msg.DestDomain, address))
	return true
}

// filterLowTransfers returns true if the amount being transferred to the destination chain is lower than the min-mint-amount configured
func filterLowTransfers(cfg *types.Config, logger log.Logger, msg *types.MessageState) bool {
	bm, err := new(cctptypes.BurnMessage).Parse(msg.MsgBody)
	if err != nil {
		logger.Info("This is not a burn message", "err", err)
		return true
	}

	// TODO: not assume that "noble" is domain 4, add "domain" to the noble chain config
	var minBurnAmount uint64
	if msg.DestDomain == types.Domain(4) {
		nobleCfg, ok := cfg.Chains["noble"].(*noble.ChainConfig)
		if !ok {
			logger.Info("Chain named 'noble' not found in config, filtering transaction")
			return true
		}
		minBurnAmount = nobleCfg.MinMintAmount
	} else {
		for _, chain := range cfg.Chains {
			c, ok := chain.(*ethereum.ChainConfig)
			if !ok {
				// noble chain, handled above
				continue
			}
			if c.Domain == msg.DestDomain {
				minBurnAmount = c.MinMintAmount
			}
		}
	}

	if bm.Amount.LT(math.NewIntFromUint64(minBurnAmount)) {
		logger.Info(
			"Filtered tx because the transfer amount is less than the minimum allowed amount",
			"dest domain", msg.DestDomain,
			"source_domain", msg.SourceDomain,
			"source_tx", msg.SourceTxHash,
			"amount", bm.Amount,
			"min_amount", minBurnAmount,
		)
		return true
	}

	return false
}

// getMintRecipientAddress extracts the mint recipient address from a MessageState
// The mint recipient is in the BurnMessage (MessageBody), stored as 32 bytes
// For Ethereum chains (domains 0,1,2,3), returns hex address (0x...) - uses last 20 bytes
// For Noble (domain 4), returns bech32 address (noble1...) - uses last 20 bytes
// For Solana (domain 5), returns base58 address - uses full 32 bytes
func getMintRecipientAddress(msg *types.MessageState) (string, error) {
	// Parse the BurnMessage from the MessageBody
	burnMsg, err := new(cctptypes.BurnMessage).Parse(msg.MsgBody)
	if err != nil {
		return "", fmt.Errorf("failed to parse burn message: %w", err)
	}

	// If destination domain is Solana (5), use full 32 bytes and encode as base58
	if msg.DestDomain == types.Domain(5) {
		if len(burnMsg.MintRecipient) != 32 {
			return "", fmt.Errorf("Solana address must be 32 bytes, got %d bytes", len(burnMsg.MintRecipient))
		}
		// Solana addresses are base58-encoded 32-byte public keys
		return base58.Encode(burnMsg.MintRecipient), nil
	}

	// For Ethereum and Noble, the address is 20 bytes padded with 12 leading zeros
	if len(burnMsg.MintRecipient) < 20 {
		return "", fmt.Errorf("mint recipient field too short: %d bytes", len(burnMsg.MintRecipient))
	}

	// Extract the last 20 bytes (the actual address)
	addressBytes := burnMsg.MintRecipient[len(burnMsg.MintRecipient)-20:]

	// If destination domain is Noble (4), convert to bech32 address
	if msg.DestDomain == types.Domain(4) {
		bech32Addr, err := bech32.ConvertAndEncode("noble", addressBytes)
		if err != nil {
			return "", fmt.Errorf("failed to convert Noble address to bech32: %w", err)
		}
		return bech32Addr, nil
	}

	// For Ethereum chains, return hex address
	return fmt.Sprintf("0x%x", addressBytes), nil
}

// normalizeAddress normalizes an address for comparison
// For hex addresses (0x...), converts to lowercase and ensures 0x prefix
// For bech32 addresses (noble1...), converts to lowercase
// For Solana addresses (base58), keeps as-is (base58 is case-sensitive)
func normalizeAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	
	// If it's a bech32 address (starts with "noble1"), convert to lowercase
	lowerAddr := strings.ToLower(addr)
	if strings.HasPrefix(lowerAddr, "noble1") {
		return lowerAddr
	}
	
	// If it's a hex address (starts with "0x" or looks like hex), normalize it
	if strings.HasPrefix(lowerAddr, "0x") {
		return lowerAddr
	}
	
	// Check if it might be a hex address without 0x prefix (40 hex chars = 20 bytes)
	if len(addr) == 40 {
		// Try to validate it's hex
		isHex := true
		for _, c := range addr {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				isHex = false
				break
			}
		}
		if isHex {
			return "0x" + lowerAddr
		}
	}
	
	// For Solana addresses (base58, typically 32-44 chars), keep as-is
	// Base58 addresses are case-sensitive, so we don't normalize them
	// They don't start with 0x or noble1, and are typically 32-44 characters
	if len(addr) >= 32 && len(addr) <= 44 {
		return addr
	}
	
	// Default: assume it's a hex address without prefix
	return "0x" + lowerAddr
}

// filterNonWhitelistedMintRecipients returns true if the mint recipient is not in the whitelist
// If the whitelist is empty, no filtering is performed (returns false)
func filterNonWhitelistedMintRecipients(cfg *types.Config, logger log.Logger, msg *types.MessageState) bool {
	// If whitelist is empty, don't filter
	if len(cfg.MintRecipientWhitelist) == 0 {
		return false
	}

	mintRecipientAddr, err := getMintRecipientAddress(msg)
	if err != nil {
		logger.Error("Failed to extract mint recipient address, filtering message", "error", err, "source_tx", msg.SourceTxHash)
		return true
	}

	normalizedRecipient := normalizeAddress(mintRecipientAddr)

	// Check if mint recipient is in whitelist
	for _, whitelistedAddr := range cfg.MintRecipientWhitelist {
		if normalizeAddress(whitelistedAddr) == normalizedRecipient {
			return false // Mint recipient is whitelisted, don't filter
		}
	}

	// Mint recipient not in whitelist, filter it out
	logger.Info(
		"Filtered tx because mint recipient is not in whitelist",
		"source_tx", msg.SourceTxHash,
		"mint_recipient", mintRecipientAddr,
		"source_domain", msg.SourceDomain,
		"dest_domain", msg.DestDomain,
	)
	return true
}

func startAPI(a *AppState) {
	logger := a.Logger
	cfg := a.Config
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	err := router.SetTrustedProxies(cfg.API.TrustedProxies) // vpn.primary.strange.love
	if err != nil {
		logger.Error("Unable to set trusted proxies on API server: " + err.Error())
		os.Exit(1)
	}

	router.GET("/tx/:txHash", getTxByHash)
	err = router.Run("localhost:8000")
	if err != nil {
		logger.Error("Unable to start API server: " + err.Error())
		os.Exit(1)
	}
}

func getTxByHash(c *gin.Context) {
	txHash := c.Param("txHash")

	domain := c.Query("domain")
	domainInt, err := strconv.ParseInt(domain, 10, 32)
	if domain != "" && err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "unable to parse domain"})
	}

	if tx, ok := State.Load(txHash); ok && domain == "" || (domain != "" && tx.Msgs[0].SourceDomain == types.Domain(uint32(domainInt))) {
		c.JSON(http.StatusOK, tx.Msgs)
		return
	}

	c.JSON(http.StatusNotFound, gin.H{"message": "message not found"})
}
