package filters

import (
	"context"
	"encoding/hex"
	"math/big"
	"os"
	"testing"

	"cosmossdk.io/log"
	"github.com/rs/zerolog"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
	"github.com/stretchr/testify/require"
)

const testDepositorAddress = "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb0"

// MockDataProvider implements DataProvider for testing
type MockDataProvider struct {
	addresses []string
	err       error
}

func (m *MockDataProvider) Name() string {
	return "mock"
}

func (m *MockDataProvider) Initialize(config map[string]interface{}) error {
	return nil
}

func (m *MockDataProvider) FetchList(ctx context.Context, key string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.addresses, nil
}

func (m *MockDataProvider) Close() error {
	return nil
}

// Helper function to create a valid BurnMessage for testing
//
//nolint:unparam // depositorAddress parameter is useful for flexibility in different test scenarios
func createBurnMessage(depositorAddress string) []byte {
	// Parse depositor address (remove 0x prefix if present)
	addressHex := depositorAddress
	if len(addressHex) > 2 && addressHex[:2] == "0x" {
		addressHex = addressHex[2:]
	}
	addressBytes, _ := hex.DecodeString(addressHex)

	// BurnMessage structure (132 bytes total):
	// - Version (4 bytes)
	// - BurnToken (32 bytes)
	// - MintRecipient (32 bytes)
	// - Amount (32 bytes)
	// - MessageSender (32 bytes) - this is where depositor address goes (last 20 bytes)
	burnMsg := make([]byte, 132)

	// Version: 0
	// (first 4 bytes stay zero)

	// BurnToken: some address (32 bytes)
	// (bytes 4-35 stay zero)

	// MintRecipient: some address (32 bytes)
	// (bytes 36-67 stay zero)

	// Amount: 1000000 (1 USDC with 6 decimals)
	amount := big.NewInt(1000000)
	amountBytes := amount.Bytes()
	copy(burnMsg[68+(32-len(amountBytes)):100], amountBytes)

	// MessageSender: pad depositor address to 32 bytes (last 20 bytes are the actual address)
	// First 12 bytes are zero padding, last 20 bytes are the address
	messageSenderStart := 100 // After Version(4) + BurnToken(32) + MintRecipient(32) + Amount(32)
	copy(burnMsg[messageSenderStart+12:messageSenderStart+32], addressBytes)

	return burnMsg
}

func TestDepositorWhitelistFilter_Whitelisted(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))
	ctx := context.Background()

	// Create filter with mock provider
	filter := NewDepositorWhitelistFilter()
	mockProvider := &MockDataProvider{
		addresses: []string{testDepositorAddress},
	}
	filter.provider = mockProvider
	filter.kvKey = "test-key"
	filter.refreshInterval = 300
	filter.logger = logger

	// Manually refresh to populate whitelist
	err := filter.refresh(ctx)
	require.NoError(t, err)

	msgBody := createBurnMessage(testDepositorAddress)
	msgState := &types.MessageState{
		SourceDomain: types.Domain(0), // Ethereum
		DestDomain:   types.Domain(4), // Noble
		SourceTxHash: "0x123",
		MsgBody:      msgBody,
	}

	// Should not filter whitelisted address
	filtered, reason, err := filter.Filter(ctx, msgState)
	require.NoError(t, err)
	require.False(t, filtered)
	require.Empty(t, reason)
}

func TestDepositorWhitelistFilter_NotWhitelisted(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))
	ctx := context.Background()

	// Create filter with mock provider containing a different address
	filter := NewDepositorWhitelistFilter()
	mockProvider := &MockDataProvider{
		addresses: []string{"0x1234567890123456789012345678901234567890"},
	}
	filter.provider = mockProvider
	filter.kvKey = "test-key"
	filter.refreshInterval = 300
	filter.logger = logger

	// Manually refresh to populate whitelist
	err := filter.refresh(ctx)
	require.NoError(t, err)

	msgBody := createBurnMessage(testDepositorAddress)
	msgState := &types.MessageState{
		SourceDomain: types.Domain(0), // Ethereum
		DestDomain:   types.Domain(4), // Noble
		SourceTxHash: "0x123",
		MsgBody:      msgBody,
	}

	// Should filter non-whitelisted address
	filtered, reason, err := filter.Filter(ctx, msgState)
	require.NoError(t, err)
	require.True(t, filtered)
	require.Contains(t, reason, "non-whitelisted depositor")
}

func TestDepositorWhitelistFilter_NonEVM(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))
	ctx := context.Background()

	// Create filter with empty whitelist
	filter := NewDepositorWhitelistFilter()
	mockProvider := &MockDataProvider{
		addresses: []string{},
	}
	filter.provider = mockProvider
	filter.kvKey = "test-key"
	filter.refreshInterval = 300
	filter.logger = logger

	err := filter.refresh(ctx)
	require.NoError(t, err)

	msgBody := createBurnMessage(testDepositorAddress)

	testCases := []struct {
		name   string
		domain types.Domain
	}{
		{"Noble (domain 4)", types.Domain(4)},
		{"Solana (domain 5)", types.Domain(5)},
		{"Monad (domain 15)", types.Domain(15)},
		{"Starknet Testnet (domain 25)", types.Domain(25)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msgState := &types.MessageState{
				SourceDomain: tc.domain,
				DestDomain:   types.Domain(0), // Ethereum
				SourceTxHash: "0x123",
				MsgBody:      msgBody,
			}

			// Should not filter for non-EVM source domains when evm_only is true
			filtered, reason, err := filter.Filter(ctx, msgState)
			require.NoError(t, err)
			require.False(t, filtered, "Non-EVM domain %d should not be filtered", tc.domain)
			require.Empty(t, reason)
		})
	}
}

func TestDepositorWhitelistFilter_InvalidMessage(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))
	ctx := context.Background()

	// Create filter
	filter := NewDepositorWhitelistFilter()
	mockProvider := &MockDataProvider{
		addresses: []string{testDepositorAddress},
	}
	filter.provider = mockProvider
	filter.kvKey = "test-key"
	filter.refreshInterval = 300
	filter.logger = logger

	err := filter.refresh(ctx)
	require.NoError(t, err)

	msgState := &types.MessageState{
		SourceDomain: types.Domain(0), // Ethereum
		DestDomain:   types.Domain(4), // Noble
		SourceTxHash: "0x123",
		MsgBody:      []byte{1, 2, 3}, // Invalid message body
	}

	// Should filter (fail-safe) when message parsing fails
	filtered, reason, err := filter.Filter(ctx, msgState)
	require.NoError(t, err)
	require.True(t, filtered)
	require.Contains(t, reason, "failed to extract depositor address")
}

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
			name:     "valid - no 0x prefix",
			input:    "742d35cc6634c0532925a3b844bc9e7595f0beb0",
			expected: "0x742d35cc6634c0532925a3b844bc9e7595f0beb0",
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
			require.Equal(t, tt.expected, result)
		})
	}
}
