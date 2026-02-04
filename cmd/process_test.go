package cmd_test

import (
	"context"
	"encoding/hex"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/cmd"
	"github.com/strangelove-ventures/noble-cctp-relayer/relayer"
	testutil "github.com/strangelove-ventures/noble-cctp-relayer/test_util"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// var a *cmd.AppState
var processingQueue chan *types.TxState
var testMetrics *relayer.PromMetrics

func init() {
	// Initialize metrics once for all tests
	testMetrics = relayer.InitPromMetrics("127.0.0.1", 9090)
}

// new log -> create state entry (not a real message, will just create state)
func TestProcessNewLog(t *testing.T) {
	a, registeredDomains := testutil.ConfigSetup(t)

	sequenceMap := types.NewSequenceMap()
	processingQueue = make(chan *types.TxState, 10)

	go cmd.StartProcessor(context.TODO(), a, registeredDomains, processingQueue, sequenceMap, nil)

	emptyBz := make([]byte, 32)
	expectedState := &types.TxState{
		TxHash: "1",
		Msgs: []*types.MessageState{
			{
				MsgBody:           make([]byte, 132),
				SourceTxHash:      "1",
				SourceDomain:      0,
				DestDomain:        4,
				DestinationCaller: emptyBz,
			},
		},
	}

	processingQueue <- expectedState

	time.Sleep(5 * time.Second)

	actualState, ok := cmd.State.Load(expectedState.TxHash)
	require.True(t, ok)
	require.Equal(t, types.Created, actualState.Msgs[0].Status)
}

// created message -> disabled cctp route -> filtered
func TestProcessDisabledCctpRoute(t *testing.T) {
	a, registeredDomains := testutil.ConfigSetup(t)

	sequenceMap := types.NewSequenceMap()
	processingQueue = make(chan *types.TxState, 10)

	go cmd.StartProcessor(context.TODO(), a, registeredDomains, processingQueue, sequenceMap, nil)

	emptyBz := make([]byte, 32)
	expectedState := &types.TxState{
		TxHash: "123",
		Msgs: []*types.MessageState{
			{
				SourceTxHash:      "123",
				IrisLookupID:      "a404f4155166a1fc7ffee145b5cac6d0f798333745289ab1db171344e226ef0c",
				Status:            types.Created,
				SourceDomain:      0,
				DestDomain:        5, // not configured
				DestinationCaller: emptyBz,
			},
		},
	}

	processingQueue <- expectedState

	time.Sleep(2 * time.Second)

	actualState, ok := cmd.State.Load(expectedState.TxHash)
	require.True(t, ok)
	require.Equal(t, types.Filtered, actualState.Msgs[0].Status)
}

// created message -> different destination caller -> filtered
func TestProcessInvalidDestinationCaller(t *testing.T) {
	a, registeredDomains := testutil.ConfigSetup(t)

	sequenceMap := types.NewSequenceMap()
	processingQueue = make(chan *types.TxState, 10)

	go cmd.StartProcessor(context.TODO(), a, registeredDomains, processingQueue, sequenceMap, nil)

	nonEmptyBytes := make([]byte, 31)
	nonEmptyBytes = append(nonEmptyBytes, 0x1)

	expectedState := &types.TxState{
		TxHash: "123",
		Msgs: []*types.MessageState{
			{
				SourceTxHash:      "123",
				IrisLookupID:      "a404f4155166a1fc7ffee145b5cac6d0f798333745289ab1db171344e226ef0c",
				Status:            types.Created,
				SourceDomain:      0,
				DestDomain:        4,
				DestinationCaller: nonEmptyBytes,
			},
		},
	}

	processingQueue <- expectedState

	time.Sleep(2 * time.Second)

	actualState, ok := cmd.State.Load(expectedState.TxHash)
	require.True(t, ok)
	require.Equal(t, types.Filtered, actualState.Msgs[0].Status)
}

// we want to filter out the transaction if the route is not enabled
func TestFilterDisabledCCTPRoutes(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))

	var msgState types.MessageState

	cfg := types.Config{
		EnabledRoutes: map[types.Domain][]types.Domain{
			0: {1, 2},
		},
	}

	// test enabled dest domain
	msgState = types.MessageState{
		SourceDomain: types.Domain(0),
		DestDomain:   types.Domain(1),
	}
	filterTx := cmd.FilterDisabledCCTPRoutes(&cfg, logger, &msgState)
	require.False(t, filterTx)

	// test NOT enabled dest domain
	msgState = types.MessageState{
		SourceDomain: types.Domain(0),
		DestDomain:   types.Domain(3),
	}
	filterTx = cmd.FilterDisabledCCTPRoutes(&cfg, logger, &msgState)
	require.True(t, filterTx)

	// test NOT enabled source domain
	msgState = types.MessageState{
		SourceDomain: types.Domain(3),
		DestDomain:   types.Domain(1),
	}
	filterTx = cmd.FilterDisabledCCTPRoutes(&cfg, logger, &msgState)
	require.True(t, filterTx)
}

const testDepositorAddress = "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb0"

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

// Test filterNonWhitelistedDepositors with whitelist disabled
func TestFilterNonWhitelistedDepositors_Disabled(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))

	// Ensure whitelist is disabled
	cmd.SetWhitelistManagerForTesting(nil)

	msgBody := createBurnMessage(testDepositorAddress)

	msgState := &types.MessageState{
		SourceDomain: types.Domain(0), // Ethereum
		DestDomain:   types.Domain(4), // Noble
		SourceTxHash: "0x123",
		MsgBody:      msgBody,
	}

	// Should not filter when whitelist is disabled
	filtered := cmd.FilterNonWhitelistedDepositors(logger, msgState, testMetrics)
	require.False(t, filtered)
}

// Test filterNonWhitelistedDepositors with whitelisted address
func TestFilterNonWhitelistedDepositors_Whitelisted(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))

	// Create a whitelist manager with a test address
	wm := types.NewWhitelistManager("", "test-key", 300, logger)

	// Manually populate whitelist for testing
	wm.SetAddressesForTesting([]string{testDepositorAddress})

	cmd.SetWhitelistManagerForTesting(wm)

	msgBody := createBurnMessage(testDepositorAddress)

	msgState := &types.MessageState{
		SourceDomain: types.Domain(0), // Ethereum
		DestDomain:   types.Domain(4), // Noble
		SourceTxHash: "0x123",
		MsgBody:      msgBody,
	}

	// Should not filter whitelisted address
	filtered := cmd.FilterNonWhitelistedDepositors(logger, msgState, testMetrics)
	require.False(t, filtered)

	// Clean up
	cmd.SetWhitelistManagerForTesting(nil)
}

// Test filterNonWhitelistedDepositors with non-whitelisted address
func TestFilterNonWhitelistedDepositors_NotWhitelisted(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))

	// Create a whitelist manager with a different address
	wm := types.NewWhitelistManager("", "test-key", 300, logger)
	wm.SetAddressesForTesting([]string{"0x1234567890123456789012345678901234567890"})

	cmd.SetWhitelistManagerForTesting(wm)

	// Use a different depositor that's not in the whitelist
	msgBody := createBurnMessage(testDepositorAddress)

	msgState := &types.MessageState{
		SourceDomain: types.Domain(0), // Ethereum
		DestDomain:   types.Domain(4), // Noble
		SourceTxHash: "0x123",
		MsgBody:      msgBody,
	}

	// Should filter non-whitelisted address
	filtered := cmd.FilterNonWhitelistedDepositors(logger, msgState, testMetrics)
	require.True(t, filtered)

	// Clean up
	cmd.SetWhitelistManagerForTesting(nil)
}

// Test filterNonWhitelistedDepositors with non-EVM chain (should not filter)
func TestFilterNonWhitelistedDepositors_NonEVM(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))

	// Create a whitelist manager
	wm := types.NewWhitelistManager("", "test-key", 300, logger)
	cmd.SetWhitelistManagerForTesting(wm)

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

			// Should not filter for non-EVM source domains
			filtered := cmd.FilterNonWhitelistedDepositors(logger, msgState, testMetrics)
			require.False(t, filtered, "Non-EVM domain %d should not be filtered", tc.domain)
		})
	}

	// Clean up
	cmd.SetWhitelistManagerForTesting(nil)
}

// Test filterNonWhitelistedDepositors with newer EVM chains (should filter if not whitelisted)
func TestFilterNonWhitelistedDepositors_NewerEVMChains(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))

	// Create a whitelist manager with no addresses
	wm := types.NewWhitelistManager("", "test-key", 300, logger)
	wm.SetAddressesForTesting([]string{}) // Empty whitelist
	cmd.SetWhitelistManagerForTesting(wm)

	msgBody := createBurnMessage(testDepositorAddress)

	testCases := []struct {
		name   string
		domain types.Domain
	}{
		{"Base (domain 6)", types.Domain(6)},
		{"Polygon PoS (domain 7)", types.Domain(7)},
		{"Unichain (domain 10)", types.Domain(10)},
		{"Linea (domain 11)", types.Domain(11)},
		{"BNB Smart Chain (domain 17)", types.Domain(17)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msgState := &types.MessageState{
				SourceDomain: tc.domain,
				DestDomain:   types.Domain(4), // Noble
				SourceTxHash: "0x123",
				MsgBody:      msgBody,
			}

			// Should filter for EVM source domains when address not whitelisted
			filtered := cmd.FilterNonWhitelistedDepositors(logger, msgState, testMetrics)
			require.True(t, filtered, "EVM domain %d should filter non-whitelisted address", tc.domain)
		})
	}

	// Clean up
	cmd.SetWhitelistManagerForTesting(nil)
}

// Test filterNonWhitelistedDepositors with invalid message body
func TestFilterNonWhitelistedDepositors_InvalidMessage(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))

	// Create a whitelist manager
	wm := types.NewWhitelistManager("", "test-key", 300, logger)
	cmd.SetWhitelistManagerForTesting(wm)

	msgState := &types.MessageState{
		SourceDomain: types.Domain(0), // Ethereum
		DestDomain:   types.Domain(4), // Noble
		SourceTxHash: "0x123",
		MsgBody:      []byte{1, 2, 3}, // Invalid message body
	}

	// Should filter (fail-safe) when message parsing fails
	filtered := cmd.FilterNonWhitelistedDepositors(logger, msgState, testMetrics)
	require.True(t, filtered)

	// Clean up
	cmd.SetWhitelistManagerForTesting(nil)
}
