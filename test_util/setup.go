package testutil

import (
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"

	"github.com/strangelove-ventures/noble-cctp-relayer/cmd"
	"github.com/strangelove-ventures/noble-cctp-relayer/ethereum"
	"github.com/strangelove-ventures/noble-cctp-relayer/noble"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// GetEnvOrDefault returns the environment variable value or a default if not set
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func init() {
	// Try to load .env file if it exists
	if err := godotenv.Load(".env"); err != nil {
		_ = godotenv.Load("../.env")
	}
}

func ConfigSetup(t *testing.T) (a *cmd.AppState, registeredDomains map[types.Domain]types.Chain) {
	t.Helper()

	var testConfig = types.Config{
		Chains: map[string]types.ChainConfig{
			"noble": &noble.ChainConfig{
				ChainID:          "grand-1",
				RPC:              GetEnvOrDefault("NOBLE_RPC", "https://noble-rpc.polkachu.com"),
				MinterPrivateKey: "1111111111111111111111111111111111111111111111111111111111111111",
			},
			"ethereum": &ethereum.ChainConfig{
				ChainID:          11155111,
				Domain:           types.Domain(0),
				MinterPrivateKey: "1111111111111111111111111111111111111111111111111111111111111111",
				RPC:              GetEnvOrDefault("SEPOLIA_RPC", "https://ethereum-sepolia-rpc.publicnode.com"),
				WS:               GetEnvOrDefault("SEPOLIA_WS", "wss://ethereum-sepolia-rpc.publicnode.com"),
			},
		},
		Circle: types.CircleSettings{
			AttestationBaseURL: "https://iris-api-sandbox.circle.com/attestations/",
			FetchRetries:       0,
			FetchRetryInterval: 3,
		},

		EnabledRoutes: map[types.Domain][]types.Domain{
			0: {4},
			4: {0},
		},
	}

	a = cmd.NewAppState()
	a.LogLevel = "debug"
	a.InitLogger()
	a.Config = &testConfig

	registeredDomains = make(map[types.Domain]types.Chain)
	for name, cfgg := range a.Config.Chains {
		c, err := cfgg.Chain(name)
		require.NoError(t, err, "Error creating chain")

		registeredDomains[c.Domain()] = c
	}

	return a, registeredDomains
}
