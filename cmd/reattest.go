package cmd

import (
	"fmt"
	"time"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/circle"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// ReattestResult contains the outcome of re-attestation attempt.
type ReattestResult struct {
	// ShouldReattest indicates if re-attestation is needed.
	ShouldReattest bool
	// NewAttestation contains the new attestation if successful.
	NewAttestation string
	// NewExpirationBlock contains updated expiration block if available.
	NewExpirationBlock uint64
	// ExhaustedRetries indicates if max retry attempts reached.
	ExhaustedRetries bool
	// RemoveFromQueue indicates message should be removed from broadcast queue.
	RemoveFromQueue bool
}

// handleExpiringAttestation checks if Fast Transfer attestation is expiring and handles re-attestation.
// Returns result indicating what action to take.
func handleExpiringAttestation(
	msg *types.MessageState,
	cfg types.CircleSettings,
	currentBlock uint64,
	logger log.Logger,
) (*ReattestResult, error) {
	result := &ReattestResult{}

	// Not a Fast Transfer or no expiration set
	if msg.ExpirationBlock == 0 {
		return result, nil
	}

	// Calculate buffer blocks (clamp negative to zero)
	bufferBlocks := uint64(0)
	if cfg.ExpirationBufferBlocks > 0 {
		bufferBlocks = uint64(cfg.ExpirationBufferBlocks)
	}

	// Check if attestation is expiring soon
	if currentBlock+bufferBlocks < msg.ExpirationBlock {
		return result, nil // Not expiring yet
	}

	result.ShouldReattest = true

	// Check if retries exhausted
	maxRetries := cfg.ReattestMaxRetries
	if maxRetries <= 0 {
		maxRetries = 3 // Default
	}
	if msg.ReattestCount >= maxRetries {
		result.ExhaustedRetries = true
		return result, fmt.Errorf("max re-attestation attempts reached for nonce %d (attempts: %d)", msg.Nonce, msg.ReattestCount)
	}

	logger.Info(fmt.Sprintf(
		"Fast Transfer attestation expiring soon for nonce %d (current: %d, expires: %d), requesting re-attestation",
		msg.Nonce, currentBlock, msg.ExpirationBlock))

	// Request re-attestation
	newAttestation, err := circle.RequestReattestation(
		cfg.AttestationBaseURL,
		logger,
		msg.SourceDomain,
		msg.Nonce,
	)
	if err != nil {
		result.RemoveFromQueue = true
		return result, fmt.Errorf("re-attestation failed for nonce %d: %w", msg.Nonce, err)
	}

	result.NewAttestation = newAttestation.Attestation

	// Fetch updated expiration block
	updatedMsg, err := circle.GetAttestationV2Message(
		cfg.AttestationBaseURL, logger, msg.SourceTxHash, msg.SourceDomain)
	if err != nil {
		logger.Info("Failed to fetch updated expiration after re-attestation", "nonce", msg.Nonce, "error", err)
	} else if updatedMsg != nil {
		result.NewExpirationBlock = circle.ParseExpirationBlock(updatedMsg.ExpirationBlock)
	}

	logger.Info(fmt.Sprintf("Re-attestation successful for nonce %d", msg.Nonce))
	return result, nil
}

// applyReattestResult applies the re-attestation result to message state.
// Caller must hold State.Mu lock before calling.
func applyReattestResult(msg *types.MessageState, result *ReattestResult) {
	if !result.ShouldReattest {
		return
	}

	msg.ReattestCount++
	msg.LastReattestTime = time.Now()

	if result.ExhaustedRetries {
		msg.Status = types.Failed
		msg.Updated = time.Now()
		return
	}

	if result.NewAttestation != "" {
		msg.Attestation = result.NewAttestation
		msg.Updated = time.Now()
	}

	if result.NewExpirationBlock > 0 {
		msg.ExpirationBlock = result.NewExpirationBlock
	}
}

