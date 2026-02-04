package types

import (
	"context"
	"os"
	"testing"

	"cosmossdk.io/log"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// MockFilter is a test implementation of MessageFilter
type MockFilter struct {
	name          string
	shouldFilter  bool
	filterReason  string
	initializeErr error
	filterErr     error
	initialized   bool
	closed        bool
}

func (m *MockFilter) Name() string {
	return m.name
}

func (m *MockFilter) Filter(ctx context.Context, msg *MessageState) (bool, string, error) {
	if m.filterErr != nil {
		return false, "", m.filterErr
	}
	return m.shouldFilter, m.filterReason, nil
}

func (m *MockFilter) Initialize(ctx context.Context, config map[string]interface{}, logger log.Logger) error {
	if m.initializeErr != nil {
		return m.initializeErr
	}
	m.initialized = true
	return nil
}

func (m *MockFilter) Close() error {
	m.closed = true
	return nil
}

func TestFilterRegistry_Register(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))
	registry := NewFilterRegistry(logger)

	filter1 := &MockFilter{name: "filter1"}
	filter2 := &MockFilter{name: "filter2"}

	registry.Register(filter1)
	registry.Register(filter2)

	require.Equal(t, 2, len(registry.filters))
}

func TestFilterRegistry_Filter_NoMatch(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))
	registry := NewFilterRegistry(logger)

	filter := &MockFilter{
		name:         "test-filter",
		shouldFilter: false,
	}
	registry.Register(filter)

	msg := &MessageState{
		SourceDomain: Domain(0),
		DestDomain:   Domain(4),
	}

	filtered, reason := registry.Filter(context.Background(), msg)
	require.False(t, filtered)
	require.Empty(t, reason)
}

func TestFilterRegistry_Filter_Match(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))
	registry := NewFilterRegistry(logger)

	filter := &MockFilter{
		name:         "test-filter",
		shouldFilter: true,
		filterReason: "test reason",
	}
	registry.Register(filter)

	msg := &MessageState{
		SourceDomain: Domain(0),
		DestDomain:   Domain(4),
	}

	filtered, reason := registry.Filter(context.Background(), msg)
	require.True(t, filtered)
	require.Equal(t, "test reason", reason)
}

func TestFilterRegistry_Filter_MultipleFilters(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))
	registry := NewFilterRegistry(logger)

	filter1 := &MockFilter{
		name:         "filter1",
		shouldFilter: false,
	}
	filter2 := &MockFilter{
		name:         "filter2",
		shouldFilter: true,
		filterReason: "matched by filter2",
	}
	registry.Register(filter1)
	registry.Register(filter2)

	msg := &MessageState{
		SourceDomain: Domain(0),
		DestDomain:   Domain(4),
	}

	// Should match on first filter that returns true
	filtered, reason := registry.Filter(context.Background(), msg)
	require.True(t, filtered)
	require.Equal(t, "matched by filter2", reason)
}

func TestFilterRegistry_Close(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))
	registry := NewFilterRegistry(logger)

	filter1 := &MockFilter{name: "filter1"}
	filter2 := &MockFilter{name: "filter2"}
	registry.Register(filter1)
	registry.Register(filter2)

	err := registry.Close()
	require.NoError(t, err)
	require.True(t, filter1.closed)
	require.True(t, filter2.closed)
}
