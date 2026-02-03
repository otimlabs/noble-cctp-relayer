package types

type Config struct {
	Chains             map[string]ChainConfig   `yaml:"chains"`
	EnabledRoutes      map[Domain][]Domain      `yaml:"enabled-routes"`
	Circle             CircleSettings           `yaml:"circle"`
	DepositorWhitelist DepositorWhitelistConfig `yaml:"depositor-whitelist"`

	ProcessorWorkerCount  uint32 `yaml:"processor-worker-count"`
	DestinationCallerOnly bool   `yaml:"destination-caller-only"`
	API                   struct {
		TrustedProxies []string `yaml:"trusted-proxies"`
	} `yaml:"api"`
}

type ConfigWrapper struct {
	Chains             map[string]map[string]any `yaml:"chains"`
	EnabledRoutes      map[Domain][]Domain       `yaml:"enabled-routes"`
	Circle             CircleSettings            `yaml:"circle"`
	DepositorWhitelist DepositorWhitelistConfig  `yaml:"depositor-whitelist"`

	ProcessorWorkerCount  uint32 `yaml:"processor-worker-count"`
	DestinationCallerOnly bool   `yaml:"destination-caller-only"`
	API                   struct {
		TrustedProxies []string `yaml:"trusted-proxies"`
	} `yaml:"api"`
}

type CircleSettings struct {
	AttestationBaseURL string `yaml:"attestation-base-url"`
	APIVersion         string `yaml:"api-version"`
	FetchRetries       int    `yaml:"fetch-retries"`
	FetchRetryInterval int    `yaml:"fetch-retry-interval"`

	// V2/Fast Transfer settings
	EnableFastTransferMonitoring bool   `yaml:"enable-fast-transfer-monitoring"`
	ReattestMaxRetries           uint   `yaml:"reattest-max-retries"`
	ExpirationBufferBlocks       uint   `yaml:"expiration-buffer-blocks"`
	AllowanceMonitorToken        string `yaml:"allowance-monitor-token"`    // token to monitor (default: USDC)
	AllowanceMonitorInterval     uint   `yaml:"allowance-monitor-interval"` // polling interval in seconds (default: 30)
}

// GetAPIVersion returns the parsed API version
func (c *CircleSettings) GetAPIVersion() (APIVersion, error) {
	return ParseAPIVersion(c.APIVersion)
}

type DepositorWhitelistConfig struct {
	Enabled         bool   `yaml:"enabled"`
	QuickNodeAPIKey string `yaml:"quicknode-api-key"`
	QuickNodeKVKey  string `yaml:"quicknode-kv-key"` // The key name in KV store
	RefreshInterval uint   `yaml:"refresh-interval"` // seconds (default: 300 = 5 minutes if omitted or 0)
}

type ChainConfig interface {
	Chain(name string) (Chain, error)
}
