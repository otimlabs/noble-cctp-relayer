package ethereum_test

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"

	"github.com/strangelove-ventures/noble-cctp-relayer/ethereum/contracts"
	testutil "github.com/strangelove-ventures/noble-cctp-relayer/test_util"
)

func TestEthUsedNonce(t *testing.T) {
	sourceDomain := uint32(4)
	nonce := uint64(612)

	keyBytes := append(
		common.LeftPadBytes((big.NewInt(int64(sourceDomain))).Bytes(), 4),
		common.LeftPadBytes((big.NewInt(int64(nonce))).Bytes(), 8)...,
	)

	var key [32]byte
	copy(key[:], keyBytes)

	client, err := ethclient.Dial(testutil.GetEnvOrDefault("SEPOLIA_RPC", "https://ethereum-sepolia-rpc.publicnode.com"))
	require.NoError(t, err)
	defer client.Close()

	messageTransmitter, err := contracts.NewMessageTransmitter(common.HexToAddress("0x7865fAfC2db2093669d92c0F33AeEF291086BEFD"), client)
	require.NoError(t, err)

	co := &bind.CallOpts{
		Pending: true,
		Context: context.TODO(),
	}

	used, err := messageTransmitter.UsedNonces(co, key)
	require.NoError(t, err)
	require.NotNil(t, used)
}
