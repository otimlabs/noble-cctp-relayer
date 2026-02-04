package filters

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"cosmossdk.io/log"
	"github.com/ethereum/go-ethereum/common"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

const DefaultWhitelistRefreshInterval = 300 // 5 minutes

// DepositorWhitelistFilter filters messages by depositor address (EVM chains only)
type DepositorWhitelistFilter struct {
	mu              sync.RWMutex
	whitelist       map[string]bool
	provider        types.DataProvider
	kvKey           string
	refreshInterval time.Duration
	logger          log.Logger
	stopCh          chan struct{}
}

func NewDepositorWhitelistFilter() *DepositorWhitelistFilter {
	return &DepositorWhitelistFilter{
		whitelist: make(map[string]bool),
		stopCh:    make(chan struct{}),
	}
}

func (f *DepositorWhitelistFilter) Name() string {
	return "depositor-whitelist"
}

func (f *DepositorWhitelistFilter) Initialize(ctx context.Context, config map[string]interface{}, logger log.Logger) error {
	f.logger = logger

	providerName, ok := config["provider"].(string)
	if !ok {
		return fmt.Errorf("depositor-whitelist filter requires 'provider' in config")
	}

	providerConfig, ok := config["provider_config"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("depositor-whitelist filter requires 'provider_config' in config")
	}

	switch providerName {
	case "quicknode-kv":
		f.provider = types.NewQuickNodeKVProvider()
	default:
		return fmt.Errorf("unknown provider: %s", providerName)
	}

	if err := f.provider.Initialize(providerConfig); err != nil {
		return fmt.Errorf("failed to initialize provider: %w", err)
	}

	kvKey, ok := config["kv_key"].(string)
	if !ok || kvKey == "" {
		return fmt.Errorf("depositor-whitelist filter requires 'kv_key' in config")
	}
	f.kvKey = kvKey

	refreshInterval := DefaultWhitelistRefreshInterval
	if val, ok := config["refresh_interval"].(int); ok && val > 0 {
		refreshInterval = val
	}
	f.refreshInterval = time.Duration(refreshInterval) * time.Second

	if err := f.refresh(ctx); err != nil {
		f.logger.Error("Failed to fetch initial whitelist", "error", err)
		return err
	}

	f.logger.Info("Depositor whitelist filter initialized",
		"provider", providerName,
		"kv_key", f.kvKey,
		"refresh_interval", f.refreshInterval,
		"initial_count", f.Count())

	go f.startRefresh(ctx)
	return nil
}

func (f *DepositorWhitelistFilter) Filter(ctx context.Context, msg *types.MessageState) (shouldFilter bool, reason string, err error) {
	if !isEVMDomain(msg.SourceDomain) {
		return false, "", nil
	}

	depositor, err := getDepositor(msg)
	if err != nil {
		f.logger.Error("Failed to extract depositor address", "tx", msg.SourceTxHash, "error", err)
		return true, "failed to extract depositor address", nil
	}

	// Check if depositor is whitelisted
	if !f.isWhitelisted(depositor) {
		reason := fmt.Sprintf("non-whitelisted depositor: %s (source_domain=%d, dest_domain=%d)",
			depositor, msg.SourceDomain, msg.DestDomain)
		return true, reason, nil
	}

	return false, "", nil
}

// Close stops the background refresh and cleans up resources
func (f *DepositorWhitelistFilter) Close() error {
	close(f.stopCh)
	if f.provider != nil {
		return f.provider.Close()
	}
	return nil
}

// startRefresh begins the periodic whitelist refresh
func (f *DepositorWhitelistFilter) startRefresh(ctx context.Context) {
	ticker := time.NewTicker(f.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			f.logger.Info("Depositor whitelist filter stopping")
			return
		case <-f.stopCh:
			return
		case <-ticker.C:
			if err := f.refresh(ctx); err != nil {
				f.logger.Error("Failed to refresh whitelist", "error", err)
			} else {
				f.logger.Debug("Whitelist refreshed", "count", f.Count())
			}
		}
	}
}

func (f *DepositorWhitelistFilter) refresh(ctx context.Context) error {
	addresses, err := f.provider.FetchList(ctx, f.kvKey)
	if err != nil {
		return err
	}

	newWhitelist := make(map[string]bool, len(addresses))
	for _, addr := range addresses {
		normalized := normalizeAddress(addr)
		if normalized != "" {
			newWhitelist[normalized] = true
		}
	}

	f.mu.Lock()
	f.whitelist = newWhitelist
	f.mu.Unlock()

	if len(newWhitelist) == 0 {
		f.logger.Info("Whitelist is empty after refresh")
	}
	return nil
}

func (f *DepositorWhitelistFilter) isWhitelisted(address string) bool {
	normalized := normalizeAddress(address)
	if normalized == "" {
		return false
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.whitelist[normalized]
}

func (f *DepositorWhitelistFilter) Count() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.whitelist)
}

func normalizeAddress(address string) string {
	address = strings.TrimSpace(address)
	if !common.IsHexAddress(address) {
		return ""
	}
	return strings.ToLower(common.HexToAddress(address).Hex())
}

func getDepositor(msg *types.MessageState) (string, error) {
	burnMsg, err := new(types.BurnMessage).Parse(msg.MsgBody)
	if err != nil {
		return "", fmt.Errorf("failed to parse burn message: %w", err)
	}
	if len(burnMsg.MessageSender) < 20 {
		return "", fmt.Errorf("invalid MessageSender length: %d", len(burnMsg.MessageSender))
	}
	address := burnMsg.MessageSender[len(burnMsg.MessageSender)-20:]
	return "0x" + hex.EncodeToString(address), nil
}

func isEVMDomain(domain types.Domain) bool {
	// EVM-compatible chains in CCTP
	evmDomains := map[types.Domain]bool{
		0:  true, // Ethereum
		1:  true, // Avalanche
		2:  true, // OP Mainnet
		3:  true, // Arbitrum
		6:  true, // Base
		7:  true, // Polygon PoS
		10: true, // Unichain
		11: true, // Linea
		12: true, // Codex
		13: true, // Sonic
		14: true, // World Chain
		16: true, // Sei
		17: true, // BNB Smart Chain
		18: true, // XDC
		19: true, // HyperEVM
		21: true, // Ink
		22: true, // Plume
		26: true, // Arc Testnet
		// Non-EVM chains: 4 (Noble), 5 (Solana), 15 (Monad), 25 (Starknet Testnet)
	}
	return evmDomains[domain]
}
