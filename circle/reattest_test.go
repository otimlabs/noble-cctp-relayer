package circle

import (
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

var testLogger log.Logger

func init() {
	testLogger = log.NewLogger(os.Stdout, log.LevelOption(zerolog.ErrorLevel))
}

// TestHandleExpiringAttestation_NotExpiring verifies no action when attestation not expiring
func TestHandleExpiringAttestation_NotExpiring(t *testing.T) {
	msg := &types.MessageState{
		Nonce:           123,
		ExpirationBlock: 1000,
		ReattestCount:   0,
	}

	cfg := types.CircleSettings{
		AttestationBaseURL:     "https://iris-api.circle.com",
		ExpirationBufferBlocks: 100,
		ReattestMaxRetries:     3,
	}

	currentBlock := uint64(800) // Before expiration

	result, err := HandleExpiringAttestation(msg, cfg, currentBlock, testLogger)
	require.NoError(t, err)
	require.False(t, result.ShouldReattest)
	require.False(t, result.ExhaustedRetries)
	require.False(t, result.RemoveFromQueue)
}

// TestHandleExpiringAttestation_ExhaustedRetries verifies max retry handling
func TestHandleExpiringAttestation_ExhaustedRetries(t *testing.T) {
	msg := &types.MessageState{
		Nonce:           123,
		ExpirationBlock: 1000,
		ReattestCount:   3, // Already at max
	}

	cfg := types.CircleSettings{
		AttestationBaseURL:     "https://iris-api.circle.com",
		ExpirationBufferBlocks: 100,
		ReattestMaxRetries:     3,
	}

	currentBlock := uint64(950) // Within expiration buffer

	result, err := HandleExpiringAttestation(msg, cfg, currentBlock, testLogger)
	require.Error(t, err)
	require.Contains(t, err.Error(), "max re-attestation attempts reached")
	require.True(t, result.ShouldReattest)
	require.True(t, result.ExhaustedRetries)
	require.False(t, result.RemoveFromQueue)
}

// TestParseExpirationBlock verifies expiration block parsing
func TestParseExpirationBlock(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected uint64
	}{
		{"valid block", "1000", 1000},
		{"empty string", "", 0},
		{"invalid string", "invalid", 0},
		{"zero", "0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseExpirationBlock(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestApplyReattestResult_ExhaustedRetries verifies state change on exhausted retries
func TestApplyReattestResult_ExhaustedRetries(t *testing.T) {
	state := types.NewStateMap()
	msg := &types.MessageState{
		Nonce:         123,
		Status:        types.Attested,
		ReattestCount: 2,
	}

	result := &ReattestResult{
		ShouldReattest:   true,
		ExhaustedRetries: true,
	}

	ApplyReattestResult(state, msg, result)

	require.Equal(t, types.Failed, msg.Status)
	require.Equal(t, uint(3), msg.ReattestCount)
	require.False(t, msg.LastReattestTime.IsZero())
}

// TestApplyReattestResult_SuccessfulReattest verifies state update on success
func TestApplyReattestResult_SuccessfulReattest(t *testing.T) {
	state := types.NewStateMap()
	msg := &types.MessageState{
		Nonce:           123,
		Status:          types.Attested,
		ReattestCount:   1,
		Attestation:     "old-attestation",
		ExpirationBlock: 1000,
	}

	result := &ReattestResult{
		ShouldReattest:     true,
		NewAttestation:     "new-attestation",
		NewExpirationBlock: 2000,
		ExhaustedRetries:   false,
	}

	beforeTime := time.Now()
	ApplyReattestResult(state, msg, result)
	afterTime := time.Now()

	require.Equal(t, types.Attested, msg.Status)
	require.Equal(t, uint(2), msg.ReattestCount)
	require.Equal(t, "new-attestation", msg.Attestation)
	require.Equal(t, uint64(2000), msg.ExpirationBlock)
	require.True(t, msg.LastReattestTime.After(beforeTime) || msg.LastReattestTime.Equal(beforeTime))
	require.True(t, msg.LastReattestTime.Before(afterTime) || msg.LastReattestTime.Equal(afterTime))
	require.True(t, msg.Updated.After(beforeTime) || msg.Updated.Equal(beforeTime))
}
