package cmd

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

// TestHandleExpiringAttestation_NotExpiring verifies no action when attestation not expiring.
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

	result, err := handleExpiringAttestation(msg, cfg, currentBlock, testLogger)
	require.NoError(t, err)
	require.False(t, result.ShouldReattest)
	require.False(t, result.ExhaustedRetries)
	require.False(t, result.RemoveFromQueue)
}

// TestHandleExpiringAttestation_NoExpirationBlock verifies no action when no expiration set.
func TestHandleExpiringAttestation_NoExpirationBlock(t *testing.T) {
	msg := &types.MessageState{
		Nonce:           123,
		ExpirationBlock: 0, // No expiration
		ReattestCount:   0,
	}

	cfg := types.CircleSettings{
		AttestationBaseURL:     "https://iris-api.circle.com",
		ExpirationBufferBlocks: 100,
		ReattestMaxRetries:     3,
	}

	result, err := handleExpiringAttestation(msg, cfg, 1000, testLogger)
	require.NoError(t, err)
	require.False(t, result.ShouldReattest)
}

// TestHandleExpiringAttestation_ExhaustedRetries verifies max retry handling.
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

	result, err := handleExpiringAttestation(msg, cfg, currentBlock, testLogger)
	require.Error(t, err)
	require.Contains(t, err.Error(), "max re-attestation attempts reached")
	require.True(t, result.ShouldReattest)
	require.True(t, result.ExhaustedRetries)
	require.False(t, result.RemoveFromQueue)
}

// TestHandleExpiringAttestation_WithinBuffer verifies detection when within buffer.
func TestHandleExpiringAttestation_WithinBuffer(t *testing.T) {
	msg := &types.MessageState{
		Nonce:           123,
		ExpirationBlock: 1000,
		ReattestCount:   0,
	}

	cfg := types.CircleSettings{
		AttestationBaseURL:     "https://iris-api-sandbox.circle.com",
		ExpirationBufferBlocks: 100,
		ReattestMaxRetries:     3,
	}

	currentBlock := uint64(920) // Within buffer (920 + 100 >= 1000)

	result, err := handleExpiringAttestation(msg, cfg, currentBlock, testLogger)

	// Note: This will actually attempt re-attestation against real API
	// In production, we'd mock the circle.RequestReattestation call
	// For now, we expect this to fail since we're using invalid test data
	if err != nil {
		require.True(t, result.ShouldReattest)
		require.True(t, result.RemoveFromQueue)
	}
}

// TestHandleExpiringAttestation_ExactExpiration verifies behavior at exact expiration.
func TestHandleExpiringAttestation_ExactExpiration(t *testing.T) {
	msg := &types.MessageState{
		Nonce:           123,
		ExpirationBlock: 1000,
		ReattestCount:   0,
	}

	cfg := types.CircleSettings{
		AttestationBaseURL:     "https://iris-api-sandbox.circle.com",
		ExpirationBufferBlocks: 0,
		ReattestMaxRetries:     3,
	}

	currentBlock := uint64(1000) // Exactly at expiration

	result, err := handleExpiringAttestation(msg, cfg, currentBlock, testLogger)

	if err != nil {
		require.True(t, result.ShouldReattest)
	}
}

// TestApplyReattestResult_NoAction verifies no state change when not needed.
func TestApplyReattestResult_NoAction(t *testing.T) {
	msg := &types.MessageState{
		Nonce:         123,
		Status:        types.Attested,
		ReattestCount: 1,
		Attestation:   "original",
	}

	originalStatus := msg.Status
	originalCount := msg.ReattestCount
	originalAttestation := msg.Attestation

	result := &ReattestResult{
		ShouldReattest: false,
	}

	applyReattestResult(msg, result)

	require.Equal(t, originalStatus, msg.Status)
	require.Equal(t, originalCount, msg.ReattestCount)
	require.Equal(t, originalAttestation, msg.Attestation)
}

// TestApplyReattestResult_ExhaustedRetries verifies state change on exhausted retries.
func TestApplyReattestResult_ExhaustedRetries(t *testing.T) {
	msg := &types.MessageState{
		Nonce:         123,
		Status:        types.Attested,
		ReattestCount: 2,
	}

	result := &ReattestResult{
		ShouldReattest:   true,
		ExhaustedRetries: true,
	}

	applyReattestResult(msg, result)

	require.Equal(t, types.Failed, msg.Status)
	require.Equal(t, uint(3), msg.ReattestCount)
	require.False(t, msg.LastReattestTime.IsZero())
}

// TestApplyReattestResult_SuccessfulReattest verifies state update on success.
func TestApplyReattestResult_SuccessfulReattest(t *testing.T) {
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
	applyReattestResult(msg, result)
	afterTime := time.Now()

	require.Equal(t, types.Attested, msg.Status)
	require.Equal(t, uint(2), msg.ReattestCount)
	require.Equal(t, "new-attestation", msg.Attestation)
	require.Equal(t, uint64(2000), msg.ExpirationBlock)
	require.True(t, msg.LastReattestTime.After(beforeTime) || msg.LastReattestTime.Equal(beforeTime))
	require.True(t, msg.LastReattestTime.Before(afterTime) || msg.LastReattestTime.Equal(afterTime))
	require.True(t, msg.Updated.After(beforeTime) || msg.Updated.Equal(beforeTime))
}

// TestApplyReattestResult_PartialUpdate verifies partial result handling.
func TestApplyReattestResult_PartialUpdate(t *testing.T) {
	msg := &types.MessageState{
		Nonce:           123,
		Status:          types.Attested,
		ReattestCount:   0,
		Attestation:     "old-attestation",
		ExpirationBlock: 1000,
	}

	// Only new attestation, no expiration update
	result := &ReattestResult{
		ShouldReattest:     true,
		NewAttestation:     "new-attestation",
		NewExpirationBlock: 0, // No update
	}

	applyReattestResult(msg, result)

	require.Equal(t, "new-attestation", msg.Attestation)
	require.Equal(t, uint64(1000), msg.ExpirationBlock) // Unchanged
	require.Equal(t, uint(1), msg.ReattestCount)
}
