package types

import (
	"context"
	"strings"
	"sync"
	"time"

	"cosmossdk.io/log"
	"github.com/ethereum/go-ethereum/common"
)

const (
	// DefaultWhitelistRefreshInterval is the default refresh interval in seconds (5 minutes)
	DefaultWhitelistRefreshInterval = 300
)

// WhitelistManager manages the in-memory cache of whitelisted depositor addresses
type WhitelistManager struct {
	mu              sync.RWMutex
	whitelist       map[string]bool // normalized addresses (lowercase)
	kvClient        *QuickNodeKVClient
	kvKey           string
	refreshInterval time.Duration
	logger          log.Logger
}

// NewWhitelistManager creates a new whitelist manager
func NewWhitelistManager(apiKey, kvKey string, refreshInterval uint, logger log.Logger) *WhitelistManager {
	// Apply default if not set or invalid
	if refreshInterval == 0 {
		refreshInterval = DefaultWhitelistRefreshInterval
		logger.Info("Using default whitelist refresh interval", "interval_seconds", refreshInterval)
	}

	return &WhitelistManager{
		whitelist:       make(map[string]bool),
		kvClient:        NewQuickNodeKVClient(apiKey),
		kvKey:           kvKey,
		refreshInterval: time.Duration(refreshInterval) * time.Second, //nolint:gosec // G115: refreshInterval is config value, overflow extremely unlikely
		logger:          logger,
	}
}

// Start begins the background refresh goroutine
func (wm *WhitelistManager) Start(ctx context.Context) {
	// Initial fetch
	if err := wm.refresh(); err != nil {
		wm.logger.Error("Failed to fetch initial whitelist", "error", err)
	} else {
		wm.logger.Info("Initial whitelist loaded", "count", wm.Count())
	}

	// Start periodic refresh
	ticker := time.NewTicker(wm.refreshInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				wm.logger.Info("Whitelist manager stopping")
				return
			case <-ticker.C:
				if err := wm.refresh(); err != nil {
					wm.logger.Error("Failed to refresh whitelist", "error", err)
				} else {
					wm.logger.Debug("Whitelist refreshed", "count", wm.Count())
				}
			}
		}
	}()
}

// refresh fetches the latest whitelist from QuickNode and updates the cache
func (wm *WhitelistManager) refresh() error {
	addresses, err := wm.kvClient.FetchList(wm.kvKey)
	if err != nil {
		return err
	}

	// Build new whitelist map
	newWhitelist := make(map[string]bool, len(addresses))
	for _, addr := range addresses {
		normalized := normalizeAddress(addr)
		if normalized != "" {
			newWhitelist[normalized] = true
		}
	}

	// Update cache under lock
	wm.mu.Lock()
	wm.whitelist = newWhitelist
	wm.mu.Unlock()

	if len(newWhitelist) == 0 {
		wm.logger.Info("Whitelist is empty after refresh")
	}

	return nil
}

// IsWhitelisted checks if an address is in the whitelist
func (wm *WhitelistManager) IsWhitelisted(address string) bool {
	normalized := normalizeAddress(address)
	if normalized == "" {
		return false
	}

	wm.mu.RLock()
	defer wm.mu.RUnlock()

	return wm.whitelist[normalized]
}

// Count returns the number of addresses in the whitelist
func (wm *WhitelistManager) Count() int {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	return len(wm.whitelist)
}

// SetAddressesForTesting manually sets the whitelist (for testing only)
func (wm *WhitelistManager) SetAddressesForTesting(addresses []string) {
	newWhitelist := make(map[string]bool, len(addresses))
	for _, addr := range addresses {
		normalized := normalizeAddress(addr)
		if normalized != "" {
			newWhitelist[normalized] = true
		}
	}

	wm.mu.Lock()
	wm.whitelist = newWhitelist
	wm.mu.Unlock()
}

// normalizeAddress converts an address to lowercase and validates format using go-ethereum
func normalizeAddress(address string) string {
	// Trim whitespace
	address = strings.TrimSpace(address)

	// Validate using go-ethereum's common package
	if !common.IsHexAddress(address) {
		return ""
	}

	// Convert to common.Address and back to hex, then lowercase for case-insensitive matching
	return strings.ToLower(common.HexToAddress(address).Hex())
}
