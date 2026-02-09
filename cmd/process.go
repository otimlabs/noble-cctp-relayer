package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/circle"
	"github.com/strangelove-ventures/noble-cctp-relayer/filters"
	"github.com/strangelove-ventures/noble-cctp-relayer/relayer"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// State and Store map the iris api lookup id -> MessageState
// State represents all in progress burns/mints
// Store represents terminal states
var State = types.NewStateMap()

// SequenceMap maps the domain -> the equivalent minter account sequence or nonce
var sequenceMap = types.NewSequenceMap()

// FilterRegistry holds all registered message filters
var FilterRegistry *types.FilterRegistry

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

			if err := initializeFilters(cmd.Context(), cfg, logger, registeredDomains); err != nil {
				return fmt.Errorf("failed to initialize filters: %w", err)
			}

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

			if FilterRegistry != nil {
				if err := FilterRegistry.Close(); err != nil {
					logger.Error("Error closing filter registry", "error", err)
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
				if metrics != nil {
					metrics.IncAttestation("observed", fmt.Sprint(msg.SourceDomain), fmt.Sprint(msg.DestDomain))
				}
			}
		}

		var broadcastMsgs = make(map[types.Domain][]*types.MessageState)
		var requeue bool

		apiVersion, apiErr := cfg.Circle.GetAPIVersion()
		if apiErr != nil {
			logger.Debug("Failed to get API version", "error", apiErr)
		}

		for _, msg := range tx.Msgs {
			srcDomain := fmt.Sprint(msg.SourceDomain)
			destDomain := fmt.Sprint(msg.DestDomain)

			// Run all filters through the filter registry
			shouldFilter := false
			var filterReason string

			if FilterRegistry != nil {
				filtered, reason := FilterRegistry.Filter(ctx, msg)
				if filtered {
					shouldFilter = true
					filterReason = reason
				}
			}

			// Mark as filtered if any filter matched
			if shouldFilter {
				State.Mu.Lock()
				prevStatus := msg.Status
				msg.Status = types.Filtered
				State.Mu.Unlock()
				// Only increment metric on first transition to filtered
				if metrics != nil && prevStatus != types.Filtered {
					metrics.IncAttestation("filtered", srcDomain, destDomain)
				}
				if filterReason != "" {
					logger.Info("Message filtered", "tx", msg.SourceTxHash, "reason", filterReason)
				}
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
					if metrics != nil {
						metrics.IncAttestation("pending", srcDomain, destDomain)
						metrics.IncPending(srcDomain, destDomain)
					}
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
					prevStatus := msg.Status
					msg.Status = types.Attested
					msg.Attestation = response.Attestation
					msg.Updated = time.Now()
					State.Mu.Unlock()
					if metrics != nil {
						metrics.IncAttestation("complete", srcDomain, destDomain)
						if prevStatus == types.Pending {
							metrics.DecPending(srcDomain, destDomain)
						}
					}

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
					if metrics != nil {
						metrics.IncAttestation("failed", srcDomain, destDomain)
					}
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

			if metrics != nil {
				for _, msg := range msgs {
					srcDomain := fmt.Sprint(msg.SourceDomain)
					destDomain := fmt.Sprint(domain)
					metrics.IncAttestation("minted", srcDomain, destDomain)
				}
			}
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

// initializeFilters creates and initializes the filter registry with configured filters
func initializeFilters(ctx context.Context, cfg *types.Config, logger log.Logger, registeredDomains map[types.Domain]types.Chain) error {
	FilterRegistry = types.NewFilterRegistry(logger)

	// Register base filters as plugins
	routeFilter := filters.NewRouteFilter()
	if err := routeFilter.Initialize(ctx, map[string]interface{}{
		"enabled_routes": cfg.EnabledRoutes,
	}, logger); err != nil {
		return fmt.Errorf("failed to initialize route filter: %w", err)
	}
	FilterRegistry.Register(routeFilter)

	destCallerFilter := filters.NewDestinationCallerFilter()
	if err := destCallerFilter.Initialize(ctx, map[string]interface{}{
		"registered_domains":      registeredDomains,
		"destination_caller_only": cfg.DestinationCallerOnly,
	}, logger); err != nil {
		return fmt.Errorf("failed to initialize destination-caller filter: %w", err)
	}
	FilterRegistry.Register(destCallerFilter)

	lowTransferFilter := filters.NewLowTransferFilter()
	if err := lowTransferFilter.Initialize(ctx, map[string]interface{}{
		"chains": cfg.Chains,
	}, logger); err != nil {
		return fmt.Errorf("failed to initialize low-transfer filter: %w", err)
	}
	FilterRegistry.Register(lowTransferFilter)

	// Register user-configured filters from config
	for _, filterCfg := range cfg.Filters {
		if !filterCfg.Enabled {
			logger.Debug("Skipping disabled filter", "name", filterCfg.Name)
			continue
		}

		var filter types.MessageFilter
		switch filterCfg.Name {
		case "depositor-whitelist":
			filter = filters.NewDepositorWhitelistFilter()
		default:
			logger.Info("Unknown filter type, skipping", "name", filterCfg.Name)
			continue
		}

		if err := filter.Initialize(ctx, filterCfg.Config, logger); err != nil {
			return fmt.Errorf("failed to initialize filter %s: %w", filterCfg.Name, err)
		}

		FilterRegistry.Register(filter)
		logger.Info("Registered custom filter", "name", filterCfg.Name)
	}

	return nil
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
