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
)

var cfg types.Config
var logger log.Logger

func init() {
	cfg.Circle.AttestationBaseURL = testV1AttestationURL
	cfg.Circle.APIVersion = "v1"
	logger = log.NewLogger(os.Stdout, log.LevelOption(zerolog.ErrorLevel))
}

func TestV1Attestation(t *testing.T) {
	cfg.Circle.APIVersion = "v1"
	cfg.Circle.AttestationBaseURL = testV1AttestationURL

	// Valid attestation (with and without 0x prefix, with and without trailing slash)
	for _, hash := range []string{testMessageHash, "0x" + testMessageHash} {
		resp := circle.CheckAttestation(cfg.Circle, logger, hash, "", 0, 4)
		require.NotNil(t, resp)
		require.Equal(t, "complete", resp.Status)
	}

	// Not found
	resp := circle.CheckAttestation(cfg.Circle, logger, "not an attestation", "", 0, 4)
	require.Nil(t, resp)
}

func TestV2Attestation(t *testing.T) {
	cfg.Circle.APIVersion = "v2"

	// Test URL normalization (with and without trailing slash, with /attestations suffix)
	for _, url := range []string{testV2BaseURL, testV2BaseURL + "/", testV1AttestationURL} {
		cfg.Circle.AttestationBaseURL = url
		_ = circle.CheckAttestation(cfg.Circle, logger, testMessageHash, "", 0, 4)
	}

	// Not found
	cfg.Circle.AttestationBaseURL = testV2BaseURL
	resp := circle.CheckAttestation(cfg.Circle, logger, "not an attestation", "", 0, 4)
	require.Nil(t, resp)
}

func TestAPIVersionParsing(t *testing.T) {
	// Valid versions
	for _, tc := range []struct {
		input    string
		expected types.APIVersion
	}{
		{"v1", types.APIVersionV1},
		{"V1", types.APIVersionV1},
		{"", types.APIVersionV1},
		{"v2", types.APIVersionV2},
		{"V2", types.APIVersionV2},
	} {
		v, err := types.ParseAPIVersion(tc.input)
		require.NoError(t, err, "input: %s", tc.input)
		require.Equal(t, tc.expected, v, "input: %s", tc.input)
	}

	// Invalid versions
	for _, input := range []string{"invalid", "v3", "1", "2"} {
		_, err := types.ParseAPIVersion(input)
		require.Error(t, err)
	}

	// String conversion - APIVersion is now a string type
	require.Equal(t, "v1", string(types.APIVersionV1))
	require.Equal(t, "v2", string(types.APIVersionV2))
}
