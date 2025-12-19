package circle

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// ReattestResult contains the outcome of re-attestation attempt
type ReattestResult struct {
	ShouldReattest     bool
	NewAttestation     string
	NewExpirationBlock uint64
	ExhaustedRetries   bool
	RemoveFromQueue    bool
}

// RequestReattestation requests a new attestation with a higher finality threshold
func RequestReattestation(baseURL string, logger log.Logger, sourceDomain types.Domain, nonce uint64) (*types.AttestationResponse, error) {
	baseURL = normalizeBaseURL(baseURL)
	url := fmt.Sprintf("%s/v2/reattest/%d/%d", baseURL, sourceDomain, nonce)

	logger.Info(fmt.Sprintf("Requesting re-attestation for domain %d nonce %d", sourceDomain, nonce))

	var reattestResp types.ReattestResponse
	if err := httpRequest(http.MethodPost, url, &reattestResp); err != nil {
		return nil, err
	}

	logger.Info(fmt.Sprintf("Re-attestation successful for nonce %d", nonce))
	return &types.AttestationResponse{
		Attestation: reattestResp.Attestation,
		Status:      reattestResp.Status,
	}, nil
}

// ParseExpirationBlock converts expiration block string to uint64, returns 0 on error
func ParseExpirationBlock(expirationBlock string) uint64 {
	if expirationBlock == "" {
		return 0
	}
	block, err := strconv.ParseUint(expirationBlock, 10, 64)
	if err != nil {
		return 0
	}
	return block
}

// HandleExpiringAttestation checks if Fast Transfer attestation is expiring and handles re-attestation
func HandleExpiringAttestation(
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

	// Check if attestation is expiring soon
	bufferBlocks := uint64(cfg.ExpirationBufferBlocks)
	if currentBlock+bufferBlocks < msg.ExpirationBlock {
		return result, nil
	}

	result.ShouldReattest = true

	// Check if retries exhausted
	maxRetries := cfg.ReattestMaxRetries
	if maxRetries == 0 {
		maxRetries = 3 // Default
	}
	if msg.ReattestCount >= maxRetries {
		result.ExhaustedRetries = true
		return result, fmt.Errorf("max re-attestation attempts reached for nonce %d (attempts: %d)", msg.Nonce, msg.ReattestCount)
	}

	logger.Info(fmt.Sprintf("Fast Transfer attestation expiring soon for nonce %d (current: %d, expires: %d), requesting re-attestation",
		msg.Nonce, currentBlock, msg.ExpirationBlock))

	// Request re-attestation
	newAttestation, err := RequestReattestation(cfg.AttestationBaseURL, logger, msg.SourceDomain, msg.Nonce)
	if err != nil {
		result.RemoveFromQueue = true
		return result, fmt.Errorf("re-attestation failed for nonce %d: %w", msg.Nonce, err)
	}

	result.NewAttestation = newAttestation.Attestation

	// Fetch updated expiration block
	if updatedMsg, err := GetAttestationV2Message(cfg.AttestationBaseURL, logger, msg.SourceTxHash, msg.SourceDomain); err != nil {
		logger.Info("Failed to fetch updated expiration after re-attestation", "nonce", msg.Nonce, "error", err)
	} else if updatedMsg != nil {
		result.NewExpirationBlock = ParseExpirationBlock(updatedMsg.ExpirationBlock)
	}

	logger.Info(fmt.Sprintf("Re-attestation successful for nonce %d", msg.Nonce))
	return result, nil
}

// ApplyReattestResult applies the re-attestation result to message state with proper locking
func ApplyReattestResult(state *types.StateMap, msg *types.MessageState, result *ReattestResult) {
	if !result.ShouldReattest {
		return
	}

	state.Mu.Lock()
	defer state.Mu.Unlock()

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

// RemoveMessageFromQueue removes a specific message from the broadcast queue
func RemoveMessageFromQueue(queue map[types.Domain][]*types.MessageState, msg *types.MessageState) {
	domainMsgs, exists := queue[msg.DestDomain]
	if !exists {
		return
	}

	filtered := domainMsgs[:0]
	for _, m := range domainMsgs {
		if m != msg {
			filtered = append(filtered, m)
		}
	}

	if len(filtered) == 0 {
		delete(queue, msg.DestDomain)
	} else {
		queue[msg.DestDomain] = filtered
	}
}







