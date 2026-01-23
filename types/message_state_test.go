package types_test

import (
	"context"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/pascaldekloe/etherstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testutil "github.com/strangelove-ventures/noble-cctp-relayer/test_util"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// TODO: update so it doesn't rely on block history
func TestToMessageStateSuccess(t *testing.T) {
	messageTransmitter, err := os.Open("../ethereum/abi/MessageTransmitter.json")
	require.NoError(t, err)

	messageTransmitterABI, err := abi.JSON(messageTransmitter)
	require.NoError(t, err)

	messageSent := messageTransmitterABI.Events["MessageSent"]

	ethClient, err := ethclient.DialContext(context.Background(), testutil.GetEnvOrDefault("SEPOLIA_RPC", "https://ethereum-sepolia-rpc.publicnode.com"))
	require.NoError(t, err)

	messageTransmitterAddress := common.HexToAddress("0x26413e8157CD32011E726065a5462e97dD4d03D9")

	query := ethereum.FilterQuery{
		Addresses: []common.Address{messageTransmitterAddress},
		Topics:    [][]common.Hash{{messageSent.ID}},
		FromBlock: big.NewInt(9573853),
		ToBlock:   big.NewInt(9573853),
	}

	etherReader := etherstream.Reader{Backend: ethClient}

	_, _, history, err := etherReader.QueryWithHistory(context.Background(), &query)
	require.NoError(t, err)

	messageState, err := types.EvmLogToMessageState(messageTransmitterABI, messageSent, &history[0])

	require.NoError(t, err)
	assert.Equal(t, "8bca2ecf8c2478597e1346c6ee2f7187e27e16d127f7bdfed67c8004a99ef306", messageState.IrisLookupID)
	assert.Equal(t, types.Domain(0), messageState.SourceDomain)
	assert.Equal(t, types.Domain(4), messageState.DestDomain)
	assert.Equal(t, types.Created, messageState.Status)
	assert.Equal(t, "0xd50e6984c74f22bb0ff0c3fb6f893f7c23b90a8f57a1941aaf9f7b0e0f78dc4d", messageState.SourceTxHash)
}

func TestStateHandling(t *testing.T) {
	ms := &types.MessageState{IrisLookupID: "test123"}
	assert.Equal(t, "test123", ms.IrisLookupID)
}
