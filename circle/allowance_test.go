package circle

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// TestNewAllowanceState verifies state initialization
func TestNewAllowanceState(t *testing.T) {
	state := NewAllowanceState()
	require.NotNil(t, state)
	require.NotNil(t, state.allowances)

	// Verify empty state
	result := state.Get(types.Domain(0))
	require.Nil(t, result)
}

// TestAllowanceState_SetGet verifies state management
func TestAllowanceState_SetGet(t *testing.T) {
	state := NewAllowanceState()

	allowance := &types.FastTransferAllowance{
		SourceDomain: "0",
		Token:        "USDC",
		Allowance:    "1000000",
		MaxAllowance: "5000000",
	}

	// Set and get
	state.Set(types.Domain(0), allowance)
	result := state.Get(types.Domain(0))

	require.NotNil(t, result)
	require.Equal(t, allowance.SourceDomain, result.SourceDomain)
	require.Equal(t, allowance.Token, result.Token)
	require.Equal(t, allowance.Allowance, result.Allowance)
	require.Equal(t, allowance.MaxAllowance, result.MaxAllowance)
}

// TestNewAllowanceMonitor_Defaults verifies default values
func TestNewAllowanceMonitor_Defaults(t *testing.T) {
	cfg := types.CircleSettings{
		AttestationBaseURL: "https://iris-api.circle.com",
	}

	domains := []types.Domain{0, 1}
	monitor := NewAllowanceMonitor(cfg, testLogger, domains, nil)

	require.NotNil(t, monitor)
	require.Equal(t, "USDC", monitor.token)
	require.Equal(t, 30*time.Second, monitor.interval)
	require.Equal(t, cfg.AttestationBaseURL, monitor.baseURL)
	require.Equal(t, domains, monitor.domains)
}

// TestNewAllowanceMonitor_CustomSettings verifies custom configuration
func TestNewAllowanceMonitor_CustomSettings(t *testing.T) {
	cfg := types.CircleSettings{
		AttestationBaseURL:       "https://iris-api.circle.com",
		AllowanceMonitorToken:    "EURC",
		AllowanceMonitorInterval: 60,
	}

	domains := []types.Domain{0}
	monitor := NewAllowanceMonitor(cfg, testLogger, domains, nil)

	require.NotNil(t, monitor)
	require.Equal(t, "EURC", monitor.token)
	require.Equal(t, 60*time.Second, monitor.interval)
}

// TestAllowanceState_ConcurrentAccess verifies thread-safe operations
func TestAllowanceState_ConcurrentAccess(t *testing.T) {
	state := NewAllowanceState()

	allowance1 := &types.FastTransferAllowance{SourceDomain: "0", Allowance: "1000000"}
	allowance2 := &types.FastTransferAllowance{SourceDomain: "1", Allowance: "2000000"}

	// Concurrent writes
	go func() {
		for i := 0; i < 10; i++ {
			state.Set(types.Domain(0), allowance1)
		}
	}()
	go func() {
		for i := 0; i < 10; i++ {
			state.Set(types.Domain(1), allowance2)
		}
	}()

	// Brief sleep to ensure goroutines complete
	time.Sleep(10 * time.Millisecond)

	// Verify final state
	require.Equal(t, "1000000", state.Get(types.Domain(0)).Allowance)
	require.Equal(t, "2000000", state.Get(types.Domain(1)).Allowance)
}
