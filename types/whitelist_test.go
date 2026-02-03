package types

import (
	"testing"
	"time"

	"cosmossdk.io/log"
)

// TestNewWhitelistManager_DefaultInterval verifies that a default refresh interval
// is applied when the config value is 0 or omitted, preventing time.NewTicker panic
func TestNewWhitelistManager_DefaultInterval(t *testing.T) {
	logger := log.NewNopLogger()

	tests := []struct {
		name                string
		inputInterval       uint
		expectedInterval    time.Duration
		expectDefaultLogged bool
	}{
		{
			name:                "zero interval uses default",
			inputInterval:       0,
			expectedInterval:    DefaultWhitelistRefreshInterval * time.Second,
			expectDefaultLogged: true,
		},
		{
			name:                "explicit interval is preserved",
			inputInterval:       60,
			expectedInterval:    60 * time.Second,
			expectDefaultLogged: false,
		},
		{
			name:                "large interval is preserved",
			inputInterval:       3600,
			expectedInterval:    3600 * time.Second,
			expectDefaultLogged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wm := NewWhitelistManager("test-api-key", "test-kv-key", tt.inputInterval, logger)

			if wm.refreshInterval != tt.expectedInterval {
				t.Errorf("expected refresh interval %v, got %v", tt.expectedInterval, wm.refreshInterval)
			}

			// Verify the interval is positive (won't panic time.NewTicker)
			if wm.refreshInterval <= 0 {
				t.Errorf("refresh interval must be positive, got %v", wm.refreshInterval)
			}
		})
	}
}

// TestNewWhitelistManager_NoTickerPanic verifies that creating a ticker
// with the manager's interval doesn't panic
func TestNewWhitelistManager_NoTickerPanic(t *testing.T) {
	logger := log.NewNopLogger()

	// Test with zero input (should use default)
	wm := NewWhitelistManager("test-api-key", "test-kv-key", 0, logger)

	// This should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("time.NewTicker panicked with interval %v: %v", wm.refreshInterval, r)
		}
	}()

	ticker := time.NewTicker(wm.refreshInterval)
	ticker.Stop()
}

// TestNormalizeAddress verifies address normalization and validation
func TestNormalizeAddress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid lowercase address",
			input:    "0x742d35cc6634c0532925a3b844bc9e7595f0beb0",
			expected: "0x742d35cc6634c0532925a3b844bc9e7595f0beb0",
		},
		{
			name:     "valid uppercase address",
			input:    "0X742D35CC6634C0532925A3B844BC9E7595F0BEB0",
			expected: "0x742d35cc6634c0532925a3b844bc9e7595f0beb0",
		},
		{
			name:     "valid mixed case address",
			input:    "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb0",
			expected: "0x742d35cc6634c0532925a3b844bc9e7595f0beb0",
		},
		{
			name:     "address with spaces",
			input:    "  0x742d35cc6634c0532925a3b844bc9e7595f0beb0  ",
			expected: "0x742d35cc6634c0532925a3b844bc9e7595f0beb0",
		},
		{
			name:     "invalid - too short",
			input:    "0x742d35cc",
			expected: "",
		},
		{
			name:     "invalid - no 0x prefix",
			input:    "742d35cc6634c0532925a3b844bc9e7595f0beb0",
			expected: "",
		},
		{
			name:     "invalid - non-hex characters",
			input:    "0x742d35cc6634c0532925a3b844bc9e7595f0beg0",
			expected: "",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeAddress(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
