package cmd_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/cmd"
	testutil "github.com/strangelove-ventures/noble-cctp-relayer/test_util"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// var a *cmd.AppState
var processingQueue chan *types.TxState

// new log -> create state entry (not a real message, will just create state)
func TestProcessNewLog(t *testing.T) {
	a, registeredDomains := testutil.ConfigSetup(t)

	sequenceMap := types.NewSequenceMap()
	processingQueue = make(chan *types.TxState, 10)

	go cmd.StartProcessor(context.TODO(), a, registeredDomains, processingQueue, sequenceMap, nil)

	emptyBz := make([]byte, 32)
	expectedState := &types.TxState{
		TxHash: "1",
		Msgs: []*types.MessageState{
			{
				MsgBody:           make([]byte, 132),
				SourceTxHash:      "1",
				SourceDomain:      0,
				DestDomain:        4,
				DestinationCaller: emptyBz,
			},
		},
	}

	processingQueue <- expectedState

	time.Sleep(5 * time.Second)

	actualState, ok := cmd.State.Load(expectedState.TxHash)
	require.True(t, ok)
	require.Equal(t, types.Created, actualState.Msgs[0].Status)
}

// created message -> disabled cctp route -> filtered
func TestProcessDisabledCctpRoute(t *testing.T) {
	a, registeredDomains := testutil.ConfigSetup(t)

	sequenceMap := types.NewSequenceMap()
	processingQueue = make(chan *types.TxState, 10)

	cmd.FilterRegistry = types.NewFilterRegistry(a.Logger)
	cmd.FilterRegistry.Register(&routeFilterMock{
		enabledRoutes: map[types.Domain][]types.Domain{0: {1, 2, 3, 4}},
	})

	go cmd.StartProcessor(context.TODO(), a, registeredDomains, processingQueue, sequenceMap, nil)

	emptyBz := make([]byte, 32)
	expectedState := &types.TxState{
		TxHash: "123",
		Msgs: []*types.MessageState{
			{
				SourceTxHash:      "123",
				IrisLookupID:      "a404f4155166a1fc7ffee145b5cac6d0f798333745289ab1db171344e226ef0c",
				Status:            types.Created,
				SourceDomain:      0,
				DestDomain:        5, // not configured
				DestinationCaller: emptyBz,
			},
		},
	}

	processingQueue <- expectedState

	time.Sleep(2 * time.Second)

	actualState, ok := cmd.State.Load(expectedState.TxHash)
	require.True(t, ok)
	require.Equal(t, types.Filtered, actualState.Msgs[0].Status)
}

// created message -> different destination caller -> filtered
func TestProcessInvalidDestinationCaller(t *testing.T) {
	a, registeredDomains := testutil.ConfigSetup(t)

	sequenceMap := types.NewSequenceMap()
	processingQueue = make(chan *types.TxState, 10)

	cmd.FilterRegistry = types.NewFilterRegistry(a.Logger)
	cmd.FilterRegistry.Register(&destCallerFilterMock{
		registeredDomains: registeredDomains,
	})

	go cmd.StartProcessor(context.TODO(), a, registeredDomains, processingQueue, sequenceMap, nil)

	nonEmptyBytes := make([]byte, 31)
	nonEmptyBytes = append(nonEmptyBytes, 0x1)

	expectedState := &types.TxState{
		TxHash: "123",
		Msgs: []*types.MessageState{
			{
				SourceTxHash:      "123",
				IrisLookupID:      "a404f4155166a1fc7ffee145b5cac6d0f798333745289ab1db171344e226ef0c",
				Status:            types.Created,
				SourceDomain:      0,
				DestDomain:        4,
				DestinationCaller: nonEmptyBytes,
			},
		},
	}

	processingQueue <- expectedState

	time.Sleep(2 * time.Second)

	actualState, ok := cmd.State.Load(expectedState.TxHash)
	require.True(t, ok)
	require.Equal(t, types.Filtered, actualState.Msgs[0].Status)
}

func TestFilterDisabledCCTPRoutes(t *testing.T) {
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.DebugLevel))
	ctx := context.Background()

	filterRegistry := types.NewFilterRegistry(logger)
	filterRegistry.Register(&routeFilterMock{
		enabledRoutes: map[types.Domain][]types.Domain{0: {1, 2}},
	})

	tests := []struct {
		src, dst types.Domain
		want     bool
	}{
		{0, 1, false},
		{0, 3, true},
		{3, 1, true},
	}

	for _, tt := range tests {
		msg := types.MessageState{SourceDomain: tt.src, DestDomain: tt.dst}
		filtered, _ := filterRegistry.Filter(ctx, &msg)
		require.Equal(t, tt.want, filtered)
	}
}

type routeFilterMock struct {
	enabledRoutes map[types.Domain][]types.Domain
}

func (f *routeFilterMock) Name() string { return "route" }
func (f *routeFilterMock) Initialize(ctx context.Context, config map[string]interface{}, logger log.Logger) error {
	return nil
}
func (f *routeFilterMock) Filter(ctx context.Context, msg *types.MessageState) (bool, string, error) {
	destDomains, ok := f.enabledRoutes[msg.SourceDomain]
	if !ok {
		return true, "source not enabled", nil
	}
	for _, dd := range destDomains {
		if dd == msg.DestDomain {
			return false, "", nil
		}
	}
	return true, "dest not enabled", nil
}
func (f *routeFilterMock) Close() error { return nil }

type destCallerFilterMock struct {
	registeredDomains map[types.Domain]types.Chain
}

func (f *destCallerFilterMock) Name() string { return "destination-caller" }
func (f *destCallerFilterMock) Initialize(ctx context.Context, config map[string]interface{}, logger log.Logger) error {
	return nil
}
func (f *destCallerFilterMock) Filter(ctx context.Context, msg *types.MessageState) (bool, string, error) {
	chain, ok := f.registeredDomains[msg.DestDomain]
	if !ok {
		return true, "no chain for dest domain", nil
	}
	validCaller, _ := chain.IsDestinationCaller(msg.DestinationCaller)
	if validCaller {
		return false, "", nil
	}
	return true, "invalid destination caller", nil
}
func (f *destCallerFilterMock) Close() error { return nil }
