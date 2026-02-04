package types

import (
	"context"
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
)

type MockFilter struct {
	name         string
	shouldFilter bool
	filterReason string
	closed       bool
}

func (m *MockFilter) Name() string { return m.name }
func (m *MockFilter) Filter(ctx context.Context, msg *MessageState) (bool, string, error) {
	return m.shouldFilter, m.filterReason, nil
}
func (m *MockFilter) Initialize(ctx context.Context, config map[string]interface{}, logger log.Logger) error {
	return nil
}
func (m *MockFilter) Close() error {
	m.closed = true
	return nil
}

func testLogger() log.Logger {
	return log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))
}

func testMsg() *MessageState {
	return &MessageState{SourceDomain: Domain(0), DestDomain: Domain(4)}
}

func TestFilterRegistry_Register(t *testing.T) {
	registry := NewFilterRegistry(testLogger())
	registry.Register(&MockFilter{name: "filter1"})
	registry.Register(&MockFilter{name: "filter2"})
	require.Len(t, registry.filters, 2)
}

func TestFilterRegistry_Filter_NoMatch(t *testing.T) {
	registry := NewFilterRegistry(testLogger())
	registry.Register(&MockFilter{name: "test", shouldFilter: false})
	filtered, reason := registry.Filter(context.Background(), testMsg())
	require.False(t, filtered)
	require.Empty(t, reason)
}

func TestFilterRegistry_Filter_Match(t *testing.T) {
	registry := NewFilterRegistry(testLogger())
	registry.Register(&MockFilter{name: "test", shouldFilter: true, filterReason: "test reason"})
	filtered, reason := registry.Filter(context.Background(), testMsg())
	require.True(t, filtered)
	require.Equal(t, "test reason", reason)
}

func TestFilterRegistry_Filter_MultipleFilters(t *testing.T) {
	registry := NewFilterRegistry(testLogger())
	registry.Register(&MockFilter{name: "filter1", shouldFilter: false})
	registry.Register(&MockFilter{name: "filter2", shouldFilter: true, filterReason: "matched"})
	filtered, reason := registry.Filter(context.Background(), testMsg())
	require.True(t, filtered)
	require.Equal(t, "matched", reason)
}

func TestFilterRegistry_Close(t *testing.T) {
	registry := NewFilterRegistry(testLogger())
	f1, f2 := &MockFilter{name: "f1"}, &MockFilter{name: "f2"}
	registry.Register(f1)
	registry.Register(f2)
	require.NoError(t, registry.Close())
	require.True(t, f1.closed)
	require.True(t, f2.closed)
}
