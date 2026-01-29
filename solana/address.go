package solana

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gagliardetto/solana-go"
)

// HexToSolanaPublicKey converts EVM-style 32-byte hex addresses to Solana PublicKeys for cross-chain CCTP compatibility
func HexToSolanaPublicKey(hexAddr string) (solana.PublicKey, error) {
	hexAddr = strings.TrimPrefix(hexAddr, "0x")

	addrBytes, err := hex.DecodeString(hexAddr)
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("invalid hex address: %w", err)
	}

	if len(addrBytes) != 32 {
		return solana.PublicKey{}, fmt.Errorf("address must be exactly 32 bytes, got %d", len(addrBytes))
	}

	return solana.PublicKeyFromBytes(addrBytes), nil
}

// BytesToSolanaPublicKey converts 32-byte slices to Solana PublicKeys
func BytesToSolanaPublicKey(addrBytes []byte) (solana.PublicKey, error) {
	if len(addrBytes) != 32 {
		return solana.PublicKey{}, fmt.Errorf("address must be exactly 32 bytes, got %d", len(addrBytes))
	}

	return solana.PublicKeyFromBytes(addrBytes), nil
}
