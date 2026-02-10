package circle

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/relayer"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// CheckFastTransferAllowance queries v2 API for remaining Fast Transfer capacity
func CheckFastTransferAllowance(baseURL string, logger log.Logger, sourceDomain types.Domain, token string) (*types.FastTransferAllowance, error) {
	baseURL = normalizeBaseURL(baseURL)
	url := fmt.Sprintf("%s/v2/fastBurn/%s/allowance?sourceDomain=%d", baseURL, token, sourceDomain)

	logger.Debug(fmt.Sprintf("Checking Fast Transfer allowance at %s", url))

	var allowance types.FastTransferAllowance
	if err := httpRequest(http.MethodGet, url, &allowance); err != nil {
		return nil, err
	}

	logger.Info(fmt.Sprintf("Fast Transfer allowance for domain %d: %s/%s %s",
		sourceDomain, allowance.Allowance.String(), allowance.MaxAllowance.String(), token))
	return &allowance, nil
}

// AllowanceState stores Fast Transfer allowance state per domain
type AllowanceState struct {
	mu         sync.RWMutex
	allowances map[types.Domain]*types.FastTransferAllowance
}

func NewAllowanceState() *AllowanceState {
	return &AllowanceState{
		allowances: make(map[types.Domain]*types.FastTransferAllowance),
	}
}

func (a *AllowanceState) Get(domain types.Domain) *types.FastTransferAllowance {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.allowances[domain]
}

func (a *AllowanceState) Set(domain types.Domain, allowance *types.FastTransferAllowance) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.allowances[domain] = allowance
}

// AllowanceMonitor tracks Fast Transfer allowance across domains
type AllowanceMonitor struct {
	baseURL           string
	logger            log.Logger
	metrics           *relayer.PromMetrics
	registeredDomains map[types.Domain]types.Chain
	state             *AllowanceState
	domains           []types.Domain
	token             string
	interval          time.Duration
}

func NewAllowanceMonitor(cfg types.CircleSettings, logger log.Logger, domains []types.Domain, metrics *relayer.PromMetrics, registeredDomains map[types.Domain]types.Chain) *AllowanceMonitor {
	token := cfg.AllowanceMonitorToken
	if token == "" {
		token = "USDC"
	}
	interval := cfg.AllowanceMonitorInterval
	if interval == 0 {
		interval = 30
	}

	return &AllowanceMonitor{
		baseURL:           cfg.AttestationBaseURL,
		logger:            logger.With("component", "allowance-monitor"),
		metrics:           metrics,
		registeredDomains: registeredDomains,
		state:             NewAllowanceState(),
		domains:           domains,
		token:             token,
		interval:          time.Duration(interval) * time.Second,
	}
}

func (m *AllowanceMonitor) State() *AllowanceState {
	return m.state
}

func (m *AllowanceMonitor) Start(ctx context.Context) {
	m.logger.Info("Starting Fast Transfer allowance monitoring", "domains", m.domains, "interval", m.interval)
	m.queryAllowances()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Stopping Fast Transfer allowance monitoring")
			return
		case <-ticker.C:
			m.queryAllowances()
		}
	}
}

// queryAllowances fetches and updates Fast Transfer allowance for all monitored domains
func (m *AllowanceMonitor) queryAllowances() {
	for _, domain := range m.domains {
		allowance, err := CheckFastTransferAllowance(m.baseURL, m.logger, domain, m.token)
		if err != nil {
			m.logger.Error("Failed to fetch allowance", "domain", domain, "error", err)
			continue
		}
		m.state.Set(domain, allowance)

		// Export to Prometheus
		if m.metrics != nil {
			if val, err := strconv.ParseUint(allowance.Allowance.String(), 10, 64); err == nil {
				// Get chain name from domain - if registry available
				chainName := "unknown"
				if m.registeredDomains != nil {
					if chain, exists := m.registeredDomains[domain]; exists && chain != nil {
						chainName = chain.Name()
					}
				}
				m.metrics.SetFastTransferAllowance(chainName, fmt.Sprintf("%d", domain), m.token, float64(val)/1e6)
			}
		}
	}
}

// StartAllowanceMonitor starts background monitoring if v2 API and monitoring are enabled.
// Returns nil if disabled, otherwise returns monitor instance running in background goroutine.
func StartAllowanceMonitor(ctx context.Context, cfg types.CircleSettings, logger log.Logger, domains []types.Domain, metrics *relayer.PromMetrics, registeredDomains map[types.Domain]types.Chain) *AllowanceMonitor {
	apiVersion, err := cfg.GetAPIVersion()
	if err != nil {
		logger.Error("Failed to parse API version for allowance monitoring", "error", err)
		return nil
	}

	if apiVersion != types.APIVersionV2 {
		logger.Info("Fast Transfer allowance monitoring disabled (requires v2 API)")
		return nil
	}

	if !cfg.EnableFastTransferMonitoring {
		logger.Info("Fast Transfer allowance monitoring disabled by config")
		return nil
	}

	monitor := NewAllowanceMonitor(cfg, logger, domains, metrics, nil)
	go monitor.Start(ctx)
	return monitor
}
