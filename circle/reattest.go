package circle

import (
	"fmt"
	"net/http"
	"strconv"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// RequestReattestation requests a new attestation with higher finality threshold
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

// ParseExpirationBlock converts expiration block string to uint64, returns 0 on error.
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
