package filters

import (
	"context"
	"encoding/hex"
	"math/big"
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

const testAddr = "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb0"

type MockDataProvider struct {
	addresses []string
}

func (m *MockDataProvider) Name() string                                   { return "mock" }
func (m *MockDataProvider) Initialize(config map[string]interface{}) error { return nil }
func (m *MockDataProvider) FetchList(ctx context.Context, key string) ([]string, error) {
	return m.addresses, nil
}
func (m *MockDataProvider) Close() error { return nil }

func createBurnMessage(addr string) []byte {
	addrHex := addr
	if len(addrHex) > 2 && addrHex[:2] == "0x" {
		addrHex = addrHex[2:]
	}
	addrBytes, _ := hex.DecodeString(addrHex)
	burnMsg := make([]byte, 132)
	amount := big.NewInt(1000000)
	copy(burnMsg[68+(32-len(amount.Bytes())):100], amount.Bytes())
	copy(burnMsg[112:132], addrBytes)
	return burnMsg
}

func setupFilter(addresses []string) *DepositorWhitelistFilter {
	f := NewDepositorWhitelistFilter()
	f.provider = &MockDataProvider{addresses: addresses}
	f.kvKey = "test"
	f.refreshInterval = 300
	f.logger = log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))
	_ = f.refresh(context.Background())
	return f
}

func TestDepositorWhitelistFilter_Whitelisted(t *testing.T) {
	filter := setupFilter([]string{testAddr})
	msg := &types.MessageState{
		SourceDomain: types.Domain(0),
		DestDomain:   types.Domain(4),
		SourceTxHash: "0x123",
		MsgBody:      createBurnMessage(testAddr),
	}
	filtered, reason, err := filter.Filter(context.Background(), msg)
	require.NoError(t, err)
	require.False(t, filtered)
	require.Empty(t, reason)
}

func TestDepositorWhitelistFilter_NotWhitelisted(t *testing.T) {
	filter := setupFilter([]string{"0x1234567890123456789012345678901234567890"})
	msg := &types.MessageState{
		SourceDomain: types.Domain(0),
		DestDomain:   types.Domain(4),
		SourceTxHash: "0x123",
		MsgBody:      createBurnMessage(testAddr),
	}
	filtered, reason, err := filter.Filter(context.Background(), msg)
	require.NoError(t, err)
	require.True(t, filtered)
	require.Contains(t, reason, "non-whitelisted")
}

func TestDepositorWhitelistFilter_NonEVM(t *testing.T) {
	filter := setupFilter([]string{})
	for _, domain := range []types.Domain{4, 5, 15, 25} {
		msg := &types.MessageState{
			SourceDomain: domain,
			DestDomain:   types.Domain(0),
			SourceTxHash: "0x123",
			MsgBody:      createBurnMessage(testAddr),
		}
		filtered, _, err := filter.Filter(context.Background(), msg)
		require.NoError(t, err)
		require.False(t, filtered)
	}
}

func TestDepositorWhitelistFilter_InvalidMessage(t *testing.T) {
	filter := setupFilter([]string{testAddr})
	msg := &types.MessageState{
		SourceDomain: types.Domain(0),
		DestDomain:   types.Domain(4),
		SourceTxHash: "0x123",
		MsgBody:      []byte{1, 2, 3},
	}
	filtered, reason, err := filter.Filter(context.Background(), msg)
	require.NoError(t, err)
	require.True(t, filtered)
	require.Contains(t, reason, "failed to extract")
}

func TestNormalizeAddress(t *testing.T) {
	tests := []struct{ in, want string }{
		{"0x742d35cc6634c0532925a3b844bc9e7595f0beb0", "0x742d35cc6634c0532925a3b844bc9e7595f0beb0"},
		{"0X742D35CC6634C0532925A3B844BC9E7595F0BEB0", "0x742d35cc6634c0532925a3b844bc9e7595f0beb0"},
		{"  0x742d35cc6634c0532925a3b844bc9e7595f0beb0  ", "0x742d35cc6634c0532925a3b844bc9e7595f0beb0"},
		{"742d35cc6634c0532925a3b844bc9e7595f0beb0", "0x742d35cc6634c0532925a3b844bc9e7595f0beb0"},
		{"0x742d35cc", ""},
		{"0x742d35cc6634c0532925a3b844bc9e7595f0beg0", ""},
		{"", ""},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, normalizeAddress(tt.in))
	}
}
