package solana

import (
	"github.com/gagliardetto/solana-go"
)

type CCTPAccounts struct {
	MessageTransmitter solana.PublicKey
	UsedNonces         solana.PublicKey

	TokenMessenger       solana.PublicKey
	RemoteTokenMessenger solana.PublicKey
	TokenMinter          solana.PublicKey
	LocalToken           solana.PublicKey
	TokenPair            solana.PublicKey
	UserTokenAccount     solana.PublicKey
	CustodyTokenAccount  solana.PublicKey
	EventAuthority       solana.PublicKey

	TokenProgram                solana.PublicKey
	TokenMessengerMinterProgram solana.PublicKey
}

var (
	USDCMintMainnet = parsePublicKey("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	USDCMintDevnet  = parsePublicKey("4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	SPLTokenProgram = parsePublicKey("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")
)

func parsePublicKey(s string) solana.PublicKey {
	return solana.MustPublicKeyFromBase58(s)
}
