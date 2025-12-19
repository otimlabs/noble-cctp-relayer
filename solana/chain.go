package solana

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/relayer"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

var _ types.Chain = (*Solana)(nil)

type Solana struct {
	name                        string
	domain                      types.Domain
	rpcURL                      string
	wsURL                       string
	messageTransmitterAddress   string
	tokenMessengerMinterAddress string
	startBlock                  uint64
	lookbackPeriod              uint64
	privateKey                  solana.PrivateKey
	minterAddress               solana.PublicKey
	maxRetries                  int
	retryIntervalSeconds        int
	minAmount                   uint64
	MetricsDenom                string
	MetricsExponent             int

	mu sync.Mutex

	rpcClient *rpc.Client

	messageTransmitterProgram   solana.PublicKey
	tokenMessengerMinterProgram solana.PublicKey
	localTokenMint              solana.PublicKey // USDC mint on Solana

	latestBlock      uint64
	lastFlushedBlock uint64
}

func NewChain(
	name string,
	domain types.Domain,
	rpcURL string,
	wsURL string,
	messageTransmitter string,
	tokenMessengerMinter string,
	startBlock uint64,
	lookbackPeriod uint64,
	privateKeyBase58 string,
	maxRetries int,
	retryIntervalSeconds int,
	minAmount uint64,
	metricsDenom string,
	metricsExponent int,
) (*Solana, error) {
	privKey, err := solana.PrivateKeyFromBase58(privateKeyBase58)
	if err != nil {
		return nil, fmt.Errorf("unable to parse Solana private key: %w", err)
	}

	minterAddress := privKey.PublicKey()

	messageTransmitterProgram, err := solana.PublicKeyFromBase58(messageTransmitter)
	if err != nil {
		return nil, fmt.Errorf("unable to parse message transmitter program address: %w", err)
	}

	tokenMessengerMinterProgram, err := solana.PublicKeyFromBase58(tokenMessengerMinter)
	if err != nil {
		return nil, fmt.Errorf("unable to parse token messenger minter program address: %w", err)
	}

	// Default to mainnet USDC with SOLANA_USDC_MINT env var override
	localTokenMint := USDCMintMainnet
	if customMint := os.Getenv("SOLANA_USDC_MINT"); customMint != "" {
		localTokenMint, err = solana.PublicKeyFromBase58(customMint)
		if err != nil {
			return nil, fmt.Errorf("unable to parse SOLANA_USDC_MINT: %w", err)
		}
	}

	return &Solana{
		name:                        name,
		domain:                      domain,
		rpcURL:                      rpcURL,
		wsURL:                       wsURL,
		messageTransmitterAddress:   messageTransmitter,
		tokenMessengerMinterAddress: tokenMessengerMinter,
		startBlock:                  startBlock,
		lookbackPeriod:              lookbackPeriod,
		privateKey:                  privKey,
		minterAddress:               minterAddress,
		maxRetries:                  maxRetries,
		retryIntervalSeconds:        retryIntervalSeconds,
		minAmount:                   minAmount,
		MetricsDenom:                metricsDenom,
		MetricsExponent:             metricsExponent,
		messageTransmitterProgram:   messageTransmitterProgram,
		tokenMessengerMinterProgram: tokenMessengerMinterProgram,
		localTokenMint:              localTokenMint,
	}, nil
}

func (s *Solana) Name() string {
	return s.name
}

func (s *Solana) Domain() types.Domain {
	return s.domain
}

func (s *Solana) LatestBlock() uint64 {
	s.mu.Lock()
	block := s.latestBlock
	s.mu.Unlock()
	return block
}

func (s *Solana) SetLatestBlock(block uint64) {
	s.mu.Lock()
	s.latestBlock = block
	s.mu.Unlock()
}

func (s *Solana) LastFlushedBlock() uint64 {
	return s.lastFlushedBlock
}

// IsDestinationCaller validates if the relayer is authorized to process this message
func (s *Solana) IsDestinationCaller(destinationCaller []byte) (isCaller bool, readableAddress string) {
	zeroByteArr := make([]byte, 32)
	if bytes.Equal(destinationCaller, zeroByteArr) {
		return true, ""
	}

	solanaAddr, err := BytesToSolanaPublicKey(destinationCaller)
	if err != nil {
		return false, hex.EncodeToString(destinationCaller)
	}

	return solanaAddr.Equals(s.minterAddress), solanaAddr.String()
}

// InitializeClients establishes connection to Solana RPC
func (s *Solana) InitializeClients(ctx context.Context, logger log.Logger) error {
	s.rpcClient = rpc.New(s.rpcURL)

	_, err := s.rpcClient.GetHealth(ctx)
	if err != nil {
		return fmt.Errorf("unable to connect to Solana RPC: %w", err)
	}

	logger.Info("Successfully connected to Solana RPC", "url", s.rpcURL)
	return nil
}

// InitializeBroadcaster prepares the relayer for broadcasting transactions
func (s *Solana) InitializeBroadcaster(
	ctx context.Context,
	logger log.Logger,
	sequenceMap *types.SequenceMap,
) error {
	// Solana uses recent blockhash for replay protection, not nonces
	sequenceMap.Put(s.Domain(), 0)
	logger.Info("Initialized Solana broadcaster", "minter_address", s.minterAddress.String())
	return nil
}

// TrackLatestBlockHeight polls for the latest Solana slot and reports metrics
func (s *Solana) TrackLatestBlockHeight(
	ctx context.Context,
	logger log.Logger,
	metrics *relayer.PromMetrics,
) {
	ticker := time.NewTicker(6 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			slot, err := s.rpcClient.GetSlot(ctx, rpc.CommitmentFinalized)
			if err != nil {
				logger.Error("Failed to get Solana slot", "error", err)
				continue
			}

			s.SetLatestBlock(slot)

			if metrics != nil {
				metrics.SetLatestHeight(s.name, fmt.Sprint(s.domain), int64(slot))
			}
		}
	}
}

// WalletBalanceMetric tracks SOL balance of the relayer wallet for monitoring
func (s *Solana) WalletBalanceMetric(
	ctx context.Context,
	logger log.Logger,
	metrics *relayer.PromMetrics,
) {
	if metrics == nil {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			balance, err := s.rpcClient.GetBalance(ctx, s.minterAddress, rpc.CommitmentFinalized)
			if err != nil {
				logger.Error("Failed to get Solana wallet balance", "error", err)
				continue
			}

			balanceInSOL := float64(balance.Value) / 1e9
			metrics.SetWalletBalance(s.name, s.minterAddress.String(), s.MetricsDenom, balanceInSOL)
		}
	}
}

// No-Op: StartListener satisfies the Chain interface but is not needed for Solana (destination-only)
func (s *Solana) StartListener(
	ctx context.Context,
	logger log.Logger,
	processingQueue chan *types.TxState,
	flushOnlyMode bool,
	flushInterval time.Duration,
) {
	<-ctx.Done()
}

// No-Op: CloseClients cleans up RPC connections
func (s *Solana) CloseClients() error {
	return nil
}
