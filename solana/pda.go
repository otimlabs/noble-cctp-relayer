package solana

import (
	"encoding/binary"
	"fmt"
	"strconv"

	"github.com/gagliardetto/solana-go"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// DeriveCCTPAccounts derives all Program Derived Addresses required for CCTP receiveMessage
// Solana CCTP uses PDAs (deterministic addresses owned by programs) instead of contract state.
// Each PDA is derived from specific seeds + program ID, following Circle's implementation.
func DeriveCCTPAccounts(
	msg *types.MessageState,
	messageTransmitterProgram solana.PublicKey,
	tokenMessengerMinterProgram solana.PublicKey,
	localTokenMint solana.PublicKey,
) (*CCTPAccounts, error) {
	parsedMsg, err := new(types.Message).Parse(msg.MsgSentBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse message: %w", err)
	}

	burnMessage, err := new(types.BurnMessage).Parse(parsedMsg.MessageBody)
	if err != nil {
		return nil, fmt.Errorf("failed to parse burn message: %w", err)
	}

	messageTransmitter, _, err := solana.FindProgramAddress(
		[][]byte{[]byte("message_transmitter")},
		messageTransmitterProgram,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive message_transmitter PDA: %w", err)
	}

	// Nonces are grouped in buckets of 65536 for bitmap efficiency
	nonceIndex := parsedMsg.Nonce / 65536
	usedNonces, _, err := solana.FindProgramAddress(
		[][]byte{
			[]byte("used_nonces"),
			[]byte(strconv.FormatUint(nonceIndex, 10)),
		},
		messageTransmitterProgram,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive used_nonces PDA: %w", err)
	}

	sourceDomainBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(sourceDomainBytes, parsedMsg.SourceDomain)
	remoteTokenMessenger, _, err := solana.FindProgramAddress(
		[][]byte{
			[]byte("remote_token_messenger"),
			sourceDomainBytes,
		},
		tokenMessengerMinterProgram,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive remote_token_messenger PDA: %w", err)
	}

	tokenMessenger, _, err := solana.FindProgramAddress(
		[][]byte{[]byte("token_messenger")},
		tokenMessengerMinterProgram,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive token_messenger PDA: %w", err)
	}

	tokenMinter, _, err := solana.FindProgramAddress(
		[][]byte{[]byte("token_minter")},
		tokenMessengerMinterProgram,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive token_minter PDA: %w", err)
	}

	localToken, _, err := solana.FindProgramAddress(
		[][]byte{
			[]byte("local_token"),
			localTokenMint.Bytes(),
		},
		tokenMessengerMinterProgram,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive local_token PDA: %w", err)
	}

	// Remote burn token must be converted from 32-byte format to Solana PublicKey
	// for use in token_pair PDA derivation
	remoteBurnToken, err := BytesToSolanaPublicKey(burnMessage.BurnToken)
	if err != nil {
		return nil, fmt.Errorf("failed to convert burn token: %w", err)
	}

	tokenPair, _, err := solana.FindProgramAddress(
		[][]byte{
			[]byte("token_pair"),
			sourceDomainBytes,
			remoteBurnToken.Bytes(),
		},
		tokenMessengerMinterProgram,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive token_pair PDA: %w", err)
	}

	// mintRecipient must be a valid SPL token account, not a wallet address
	userTokenAccount, err := BytesToSolanaPublicKey(burnMessage.MintRecipient)
	if err != nil {
		return nil, fmt.Errorf("invalid mint recipient: %w", err)
	}

	custodyTokenAccount, _, err := solana.FindProgramAddress(
		[][]byte{
			[]byte("custody"),
			localTokenMint.Bytes(),
		},
		tokenMessengerMinterProgram,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive custody_token_account PDA: %w", err)
	}

	eventAuthority, _, err := solana.FindProgramAddress(
		[][]byte{[]byte("__event_authority")},
		tokenMessengerMinterProgram,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive event_authority PDA: %w", err)
	}

	return &CCTPAccounts{
		MessageTransmitter:          messageTransmitter,
		UsedNonces:                  usedNonces,
		TokenMessenger:              tokenMessenger,
		RemoteTokenMessenger:        remoteTokenMessenger,
		TokenMinter:                 tokenMinter,
		LocalToken:                  localToken,
		TokenPair:                   tokenPair,
		UserTokenAccount:            userTokenAccount,
		CustodyTokenAccount:         custodyTokenAccount,
		EventAuthority:              eventAuthority,
		TokenProgram:                SPLTokenProgram,
		TokenMessengerMinterProgram: tokenMessengerMinterProgram,
	}, nil
}
