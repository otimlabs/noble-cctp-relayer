package circle

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/relayer"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// parseAllowance converts allowance string to float64 (USDC with 6 decimals)
func parseAllowance(s string) (float64, error) {
	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return float64(val) / 1e6, nil // Convert from smallest unit to USDC
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
	baseURL  string
	logger   log.Logger
	metrics  *relayer.PromMetrics
	state    *AllowanceState
	domains  []types.Domain
	token    string
	interval time.Duration
}

func NewAllowanceMonitor(baseURL string, logger log.Logger, domains []types.Domain, metrics *relayer.PromMetrics) *AllowanceMonitor {
	return &AllowanceMonitor{
		baseURL:  baseURL,
		logger:   logger.With("component", "allowance-monitor"),
		metrics:  metrics,
		state:    NewAllowanceState(),
		domains:  domains,
		token:    "USDC",
		interval: 30 * time.Second,
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

func (m *AllowanceMonitor) queryAllowances() {
	for _, domain := range m.domains {
		allowance, err := CheckFastTransferAllowance(m.baseURL, m.logger, domain, m.token)
		if err != nil {
			m.logger.Error("Failed to fetch allowance", "domain", domain, "error", err)
			continue
		}
		m.state.Set(domain, allowance)

		// Export to Prometheus
		if m.metrics != nil && allowance.Allowance != "" {
			if val, err := parseAllowance(allowance.Allowance); err == nil {
				m.metrics.SetFastTransferAllowance(fmt.Sprintf("%d", domain), m.token, val)
			}
		}
	}
}

// StartAllowanceMonitor starts a background allowance monitor if v2 is enabled
func StartAllowanceMonitor(ctx context.Context, cfg types.CircleSettings, logger log.Logger, domains []types.Domain, metrics *relayer.PromMetrics) *AllowanceMonitor {
	apiVersion, err := cfg.GetAPIVersion()
	if err != nil || apiVersion != types.APIVersionV2 {
		logger.Info("Fast Transfer allowance monitoring disabled (not v2)")
		return nil
	}

	if !cfg.EnableFastTransferMonitoring {
		logger.Info("Fast Transfer allowance monitoring disabled by config")
		return nil
	}

	monitor := NewAllowanceMonitor(cfg.AttestationBaseURL, logger, domains, metrics)
	go monitor.Start(ctx)
	return monitor
}
