package circle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

const httpTimeout = 5 * time.Second

// httpGet performs a GET request and unmarshals the JSON response
func httpGet(url string, result any) error {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return json.Unmarshal(body, result)
}

// httpPost performs a POST request and unmarshals the JSON response
func httpPost(url string, result any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return json.Unmarshal(body, result)
}

// normalizeMessageHash ensures the message hash has 0x prefix
func normalizeMessageHash(hash string) string {
	if len(hash) > 2 && hash[:2] != "0x" {
		return "0x" + hash
	}
	return hash
}

// normalizeBaseURL removes trailing slashes and /attestations suffix for v2
func normalizeBaseURL(url string) string {
	url = strings.TrimSuffix(url, "/")
	return strings.TrimSuffix(url, "/attestations")
}

// CheckAttestation checks the Circle Iris API for attestation status
func CheckAttestation(cfg types.CircleSettings, logger log.Logger, irisLookupID, txHash string, sourceDomain, destDomain types.Domain) *types.AttestationResponse {
	version, err := cfg.GetAPIVersion()
	if err != nil {
		logger.Error("invalid API version", "error", err)
		return nil
	}

	switch version {
	case types.APIVersionV1:
		return checkAttestationV1(cfg.AttestationBaseURL, logger, irisLookupID)
	case types.APIVersionV2:
		return checkAttestationV2(cfg.AttestationBaseURL, logger, txHash, sourceDomain)
	default:
		return checkAttestationV1(cfg.AttestationBaseURL, logger, irisLookupID)
	}
}

// checkAttestationV1 uses the legacy v1 API: {baseURL}/{messageHash}
func checkAttestationV1(attestationURL string, logger log.Logger, irisLookupID string) *types.AttestationResponse {
	if attestationURL[len(attestationURL)-1:] != "/" {
		attestationURL += "/"
	}
	irisLookupID = normalizeMessageHash(irisLookupID)

	url := attestationURL + irisLookupID
	logger.Debug(fmt.Sprintf("Checking v1 attestation at %s", url))

	var response types.AttestationResponse
	if err := httpGet(url, &response); err != nil {
		logger.Debug("v1 attestation request failed", "error", err)
		return nil
	}

	logger.Info(fmt.Sprintf("Attestation found for %s", irisLookupID))
	return &response
}

// checkAttestationV2 uses the v2 API: {baseURL}/v2/messages/{sourceDomain}?transactionHash={txHash}
// Note: V2 API may return multiple messages per transaction. This function returns the first
// message for backwards compatibility. Use CheckAttestationV2All for multi-message handling.
func checkAttestationV2(baseURL string, logger log.Logger, txHash string, sourceDomain types.Domain) *types.AttestationResponse {
	baseURL = normalizeBaseURL(baseURL)
	txHash = normalizeMessageHash(txHash)

	url := fmt.Sprintf("%s/v2/messages/%d?transactionHash=%s", baseURL, sourceDomain, txHash)
	logger.Debug(fmt.Sprintf("Checking v2 attestation at %s", url))

	var v2Response types.AttestationResponseV2
	if err := httpGet(url, &v2Response); err != nil {
		logger.Debug("v2 attestation request failed", "error", err)
		return nil
	}

	if len(v2Response.Messages) == 0 {
		return nil
	}

	if len(v2Response.Messages) > 1 {
		logger.Info(fmt.Sprintf("V2 attestation found %d messages for tx %s, using first", len(v2Response.Messages), txHash))
	} else {
		logger.Info(fmt.Sprintf("V2 attestation found for tx %s", txHash))
	}

	msg := v2Response.Messages[0]
	return &types.AttestationResponse{
		Attestation: msg.Attestation,
		Status:      msg.Status,
	}
}

// CheckAttestationV2All fetches all messages for a transaction (v2 API supports multiple per tx)
func CheckAttestationV2All(baseURL string, logger log.Logger, txHash string, sourceDomain types.Domain) ([]types.MessageResponseV2, error) {
	baseURL = normalizeBaseURL(baseURL)
	txHash = normalizeMessageHash(txHash)

	url := fmt.Sprintf("%s/v2/messages/%d?transactionHash=%s", baseURL, sourceDomain, txHash)
	logger.Debug(fmt.Sprintf("Fetching all v2 messages at %s", url))

	var v2Response types.AttestationResponseV2
	if err := httpGet(url, &v2Response); err != nil {
		return nil, err
	}

	if len(v2Response.Messages) == 0 {
		return nil, fmt.Errorf("no messages found")
	}

	logger.Info(fmt.Sprintf("Found %d v2 messages for tx %s", len(v2Response.Messages), txHash))
	return v2Response.Messages, nil
}

// GetAttestationV2Message fetches the full v2 message including Fast Transfer expiration details
func GetAttestationV2Message(baseURL string, logger log.Logger, txHash string, sourceDomain types.Domain) (*types.MessageResponseV2, error) {
	baseURL = normalizeBaseURL(baseURL)
	txHash = normalizeMessageHash(txHash)

	url := fmt.Sprintf("%s/v2/messages/%d?transactionHash=%s", baseURL, sourceDomain, txHash)
	logger.Debug(fmt.Sprintf("Fetching v2 message details at %s", url))

	var v2Response types.AttestationResponseV2
	if err := httpGet(url, &v2Response); err != nil {
		return nil, err
	}

	if len(v2Response.Messages) == 0 {
		return nil, fmt.Errorf("no messages found")
	}

	return &v2Response.Messages[0], nil
}

// CheckFastTransferAllowance queries the Fast Transfer allowance for a domain
func CheckFastTransferAllowance(baseURL string, logger log.Logger, sourceDomain types.Domain, token string) (*types.FastTransferAllowance, error) {
	baseURL = normalizeBaseURL(baseURL)
	url := fmt.Sprintf("%s/v2/fastBurn/%s/allowance?sourceDomain=%d", baseURL, token, sourceDomain)

	logger.Debug(fmt.Sprintf("Checking Fast Transfer allowance at %s", url))

	var allowance types.FastTransferAllowance
	if err := httpGet(url, &allowance); err != nil {
		return nil, err
	}

	logger.Info(fmt.Sprintf("Fast Transfer allowance for domain %d: %s/%s %s",
		sourceDomain, allowance.Allowance, allowance.MaxAllowance, token))
	return &allowance, nil
}

// RequestReattestation requests a new attestation with higher finality threshold
func RequestReattestation(baseURL string, logger log.Logger, sourceDomain types.Domain, nonce uint64) (*types.AttestationResponse, error) {
	baseURL = normalizeBaseURL(baseURL)
	url := fmt.Sprintf("%s/v2/reattest/%d/%d", baseURL, sourceDomain, nonce)

	logger.Info(fmt.Sprintf("Requesting re-attestation for domain %d nonce %d", sourceDomain, nonce))

	var reattestResp types.ReattestResponse
	if err := httpPost(url, &reattestResp); err != nil {
		return nil, err
	}

	logger.Info(fmt.Sprintf("Re-attestation successful for nonce %d", nonce))
	return &types.AttestationResponse{
		Attestation: reattestResp.Attestation,
		Status:      reattestResp.Status,
	}, nil
}

// ParseExpirationBlock parses the expiration block string to uint64
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
