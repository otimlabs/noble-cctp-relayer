package types

import "fmt"

type Config struct {
	Chains        map[string]ChainConfig `yaml:"chains"`
	EnabledRoutes map[Domain][]Domain    `yaml:"enabled-routes"`
	Circle        CircleSettings         `yaml:"circle"`

	ProcessorWorkerCount uint32 `yaml:"processor-worker-count"`
	API                  struct {
		TrustedProxies []string `yaml:"trusted-proxies"`
	} `yaml:"api"`
}

type ConfigWrapper struct {
	Chains        map[string]map[string]any `yaml:"chains"`
	EnabledRoutes map[Domain][]Domain       `yaml:"enabled-routes"`
	Circle        CircleSettings            `yaml:"circle"`

	ProcessorWorkerCount uint32 `yaml:"processor-worker-count"`
	API                  struct {
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
	ReattestMaxRetries           int    `yaml:"reattest-max-retries"`
	ExpirationBufferBlocks       int    `yaml:"expiration-buffer-blocks"`
	AllowanceMonitorToken        string `yaml:"allowance-monitor-token"`    // v2: token to monitor (default: USDC)
	AllowanceMonitorInterval     int    `yaml:"allowance-monitor-interval"` // v2: polling interval in seconds (default: 30)
}

// GetAPIVersion returns the parsed API version
func (c *CircleSettings) GetAPIVersion() (APIVersion, error) {
	return ParseAPIVersion(c.APIVersion)
}

// Validate ensures v2-specific configs are only set when using v2 API.
func (c *CircleSettings) Validate() error {
	version, err := c.GetAPIVersion()
	if err != nil {
		return fmt.Errorf("invalid api-version: %w", err)
	}

	// V2-specific validation
	if version == APIVersionV2 {
		if c.EnableFastTransferMonitoring && c.AllowanceMonitorInterval <= 0 {
			return fmt.Errorf("allowance-monitor-interval must be positive when enable-fast-transfer-monitoring is true")
		}
		if c.ReattestMaxRetries < 0 {
			return fmt.Errorf("reattest-max-retries cannot be negative")
		}
		if c.ExpirationBufferBlocks < 0 {
			return fmt.Errorf("expiration-buffer-blocks cannot be negative")
		}
	}

	// Warn if v2-specific configs are set but using v1
	if version == APIVersionV1 {
		if c.EnableFastTransferMonitoring {
			return fmt.Errorf("enable-fast-transfer-monitoring requires api-version: v2")
		}
		if c.ReattestMaxRetries > 0 {
			return fmt.Errorf("reattest-max-retries requires api-version: v2")
		}
	}

	return nil
}

type ChainConfig interface {
	Chain(name string) (Chain, error)
}
