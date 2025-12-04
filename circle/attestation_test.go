package circle_test

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/circle"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

const (
	testV1AttestationURL = "https://iris-api-sandbox.circle.com/attestations/"
	testV2BaseURL        = "https://iris-api-sandbox.circle.com"
	testMessageHash      = "85bbf7e65a5992e6317a61f005e06d9972a033d71b514be183b179e1b47723fe"
	testMessageHash0x    = "0x85bbf7e65a5992e6317a61f005e06d9972a033d71b514be183b179e1b47723fe"
)

var cfg types.Config
var logger log.Logger

func init() {
	cfg.Circle.AttestationBaseURL = testV1AttestationURL
	cfg.Circle.APIVersion = "v1"
	logger = log.NewLogger(os.Stdout, log.LevelOption(zerolog.ErrorLevel))
}

// V1 API Tests

func TestAttestationIsReady(t *testing.T) {
	cfg.Circle.APIVersion = "v1"
	cfg.Circle.AttestationBaseURL = testV1AttestationURL
	resp := circle.CheckAttestation(cfg.Circle, logger, testMessageHash, "", 0, 4)
	require.NotNil(t, resp)
	require.Equal(t, "complete", resp.Status)
}

func TestAttestationNotFound(t *testing.T) {
	cfg.Circle.APIVersion = "v1"
	cfg.Circle.AttestationBaseURL = testV1AttestationURL
	resp := circle.CheckAttestation(cfg.Circle, logger, "not an attestation", "", 0, 4)
	require.Nil(t, resp)
}

func TestAttestationWithoutEndingSlash(t *testing.T) {
	cfg.Circle.APIVersion = "v1"
	startURL := cfg.Circle.AttestationBaseURL
	cfg.Circle.AttestationBaseURL = startURL[:len(startURL)-1]

	resp := circle.CheckAttestation(cfg.Circle, logger, testMessageHash, "", 0, 4)
	require.NotNil(t, resp)
	require.Equal(t, "complete", resp.Status)

	cfg.Circle.AttestationBaseURL = startURL
}

func TestAttestationWithLeading0x(t *testing.T) {
	cfg.Circle.APIVersion = "v1"
	cfg.Circle.AttestationBaseURL = testV1AttestationURL
	resp := circle.CheckAttestation(cfg.Circle, logger, testMessageHash0x, "", 0, 4)
	require.NotNil(t, resp)
	require.Equal(t, "complete", resp.Status)
}

// V2 API Tests

func TestV2AttestationIsReady(t *testing.T) {
	cfg.Circle.APIVersion = "v2"
	cfg.Circle.AttestationBaseURL = testV2BaseURL
	resp := circle.CheckAttestation(cfg.Circle, logger, testMessageHash, "", 0, 4)
	if resp != nil {
		require.Contains(t, []string{"complete", "pending_confirmations"}, resp.Status)
	}
}

func TestV2AttestationNotFound(t *testing.T) {
	cfg.Circle.APIVersion = "v2"
	cfg.Circle.AttestationBaseURL = testV2BaseURL
	resp := circle.CheckAttestation(cfg.Circle, logger, "not an attestation", "", 0, 4)
	require.Nil(t, resp)
}

func TestV2AttestationWithLeading0x(t *testing.T) {
	cfg.Circle.APIVersion = "v2"
	cfg.Circle.AttestationBaseURL = testV2BaseURL
	resp := circle.CheckAttestation(cfg.Circle, logger, testMessageHash0x, "", 0, 4)
	if resp != nil {
		require.Contains(t, []string{"complete", "pending_confirmations"}, resp.Status)
	}
}

func TestV2AttestationURLNormalization(t *testing.T) {
	cfg.Circle.APIVersion = "v2"
	cfg.Circle.AttestationBaseURL = testV2BaseURL + "/"
	_ = circle.CheckAttestation(cfg.Circle, logger, testMessageHash, "", 0, 4)

	cfg.Circle.AttestationBaseURL = testV1AttestationURL
	_ = circle.CheckAttestation(cfg.Circle, logger, testMessageHash, "", 0, 4)
}

// API Version Tests

func TestAPIVersionParsing(t *testing.T) {
	v, err := types.ParseAPIVersion("v1")
	require.NoError(t, err)
	require.Equal(t, types.APIVersionV1, v)

	v, err = types.ParseAPIVersion("V1")
	require.NoError(t, err)
	require.Equal(t, types.APIVersionV1, v)

	v, err = types.ParseAPIVersion("1")
	require.NoError(t, err)
	require.Equal(t, types.APIVersionV1, v)

	v, err = types.ParseAPIVersion("v2")
	require.NoError(t, err)
	require.Equal(t, types.APIVersionV2, v)

	v, err = types.ParseAPIVersion("V2")
	require.NoError(t, err)
	require.Equal(t, types.APIVersionV2, v)

	v, err = types.ParseAPIVersion("2")
	require.NoError(t, err)
	require.Equal(t, types.APIVersionV2, v)

	v, err = types.ParseAPIVersion("")
	require.NoError(t, err)
	require.Equal(t, types.APIVersionV1, v)
}

func TestAPIVersionParsingInvalid(t *testing.T) {
	_, err := types.ParseAPIVersion("invalid")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid API version")

	_, err = types.ParseAPIVersion("v3")
	require.Error(t, err)
}

func TestAPIVersionString(t *testing.T) {
	require.Equal(t, "v1", types.APIVersionV1.String())
	require.Equal(t, "v2", types.APIVersionV2.String())
}

func TestCircleSettingsGetAPIVersion(t *testing.T) {
	settings := types.CircleSettings{AttestationBaseURL: "https://iris-api.circle.com"}

	settings.APIVersion = ""
	v, err := settings.GetAPIVersion()
	require.NoError(t, err)
	require.Equal(t, types.APIVersionV1, v)

	settings.APIVersion = "v2"
	v, err = settings.GetAPIVersion()
	require.NoError(t, err)
	require.Equal(t, types.APIVersionV2, v)

	settings.APIVersion = "invalid"
	_, err = settings.GetAPIVersion()
	require.Error(t, err)
}

// Expiration Block Parsing Tests

func TestParseExpirationBlock(t *testing.T) {
	require.Equal(t, uint64(0), circle.ParseExpirationBlock(""))
	require.Equal(t, uint64(0), circle.ParseExpirationBlock("invalid"))
	require.Equal(t, uint64(12345), circle.ParseExpirationBlock("12345"))
	require.Equal(t, uint64(999999999), circle.ParseExpirationBlock("999999999"))
}
