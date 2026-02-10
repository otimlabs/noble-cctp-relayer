package filters

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"cosmossdk.io/log"

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
		// yaml.v2 unmarshals nested maps as map[interface{}]interface{}
		if rawMap, ok2 := config["provider_config"].(map[interface{}]interface{}); ok2 {
			providerConfig = make(map[string]interface{}, len(rawMap))
			for k, v := range rawMap {
				providerConfig[fmt.Sprintf("%v", k)] = v
			}
		} else {
			return fmt.Errorf("depositor-whitelist filter requires 'provider_config' in config")
		}
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
	// YAML unmarshals numbers as float64, not int
	if val, ok := config["refresh_interval"].(float64); ok && val > 0 {
		refreshInterval = int(val)
	} else if val, ok := config["refresh_interval"].(int); ok && val > 0 {
		refreshInterval = val
	}
	f.refreshInterval = time.Duration(refreshInterval) * time.Second

	if err := f.refresh(ctx); err != nil {
		f.logger.Error("Failed to fetch initial whitelist", "error", err)
		return err
	}

	initialCount := f.Count()
	allAddresses := make([]string, 0, initialCount)
	f.mu.RLock()
	for addr := range f.whitelist {
		allAddresses = append(allAddresses, addr)
	}
	f.mu.RUnlock()

	f.logger.Info("Depositor whitelist filter initialized",
		"provider", providerName,
		"kv_key", f.kvKey,
		"refresh_interval", f.refreshInterval,
		"initial_count", initialCount,
		"all_addresses", allAddresses)

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
		f.logger.Debug("Message filtered by depositor whitelist",
			"depositor", depositor,
			"source_domain", msg.SourceDomain,
			"dest_domain", msg.DestDomain,
			"tx_hash", msg.SourceTxHash)
		return true, reason, nil
	}

	f.logger.Info("Message passed depositor whitelist",
		"depositor", depositor,
		"source_domain", msg.SourceDomain,
		"dest_domain", msg.DestDomain)

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
				f.logger.Info("Whitelist refreshed", "count", f.Count())
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
	var skippedAddresses []string

	for _, addr := range addresses {
		if normalized := normalizeAddress(addr); normalized != "" {
			newWhitelist[normalized] = true
		} else {
			skippedAddresses = append(skippedAddresses, addr)
		}
	}

	if len(skippedAddresses) > 0 {
		f.logger.Error("Skipped invalid addresses during refresh",
			"skipped_count", len(skippedAddresses),
			"skipped_addresses", skippedAddresses)
	}

	f.mu.RLock()
	oldCount := len(f.whitelist)
	oldWhitelist := make(map[string]bool)
	for addr := range f.whitelist {
		oldWhitelist[addr] = true
	}
	f.mu.RUnlock()

	f.mu.Lock()
	f.whitelist = newWhitelist
	f.mu.Unlock()

	newCount := len(newWhitelist)
	addedAddresses := make([]string, 0)
	removedAddresses := make([]string, 0)

	for addr := range newWhitelist {
		if !oldWhitelist[addr] {
			addedAddresses = append(addedAddresses, addr)
		}
	}

	for addr := range oldWhitelist {
		if !newWhitelist[addr] {
			removedAddresses = append(removedAddresses, addr)
		}
	}

	if newCount == 0 {
		f.logger.Info("Whitelist is empty after refresh")
	}

	if newCount > 0 {
		allAddresses := make([]string, 0, newCount)
		for addr := range newWhitelist {
			allAddresses = append(allAddresses, addr)
		}
		f.logger.Info("Whitelist refresh addresses", "addresses", allAddresses, "total_count", newCount)
	}

	f.logger.Info("Whitelist refresh completed",
		"previous_count", oldCount,
		"new_count", newCount,
		"change", newCount-oldCount,
		"added_addresses", addedAddresses,
		"removed_addresses", removedAddresses)

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
	switch domain {
	case 4, 5, 15, 25: // Noble, Solana, Monad, Starknet Testnet
		return false
	default:
		return true
	}
}
