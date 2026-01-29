package solana

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/relayer"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// Broadcast sends CCTP mint transactions to Solana with retry logic
func (s *Solana) Broadcast(
	ctx context.Context,
	logger log.Logger,
	msgs []*types.MessageState,
	sequenceMap *types.SequenceMap,
	m *relayer.PromMetrics,
) error {
	logger = logger.With("chain", s.name, "domain", s.domain)
	var broadcastErrors error

MsgLoop:
	for _, msg := range msgs {
		attestationBytes, err := hex.DecodeString(msg.Attestation[2:])
		if err != nil {
			return errors.New("unable to decode message attestation")
		}

		for attempt := 0; attempt <= s.maxRetries; attempt++ {
			if msg.Status == types.Complete {
				continue MsgLoop
			}

			if err := s.attemptBroadcast(ctx, logger, msg, attestationBytes); err == nil {
				continue MsgLoop
			}

			if attempt != s.maxRetries {
				logger.Info(fmt.Sprintf("Retrying in %d seconds", s.retryIntervalSeconds))
				time.Sleep(time.Duration(s.retryIntervalSeconds) * time.Second)
			}
		}

		if m != nil {
			m.IncBroadcastErrors(s.name, fmt.Sprint(s.domain))
		}
		broadcastErrors = errors.Join(broadcastErrors, errors.New("reached max number of broadcast attempts"))
	}

	return broadcastErrors
}

func (s *Solana) attemptBroadcast(
	ctx context.Context,
	logger log.Logger,
	msg *types.MessageState,
	attestationBytes []byte,
) error {
	logger.Info(fmt.Sprintf("Broadcasting message from %d to %d: with source tx hash %s",
		msg.SourceDomain, msg.DestDomain, msg.SourceTxHash))

	accounts, err := DeriveCCTPAccounts(msg, s.messageTransmitterProgram, s.tokenMessengerMinterProgram, s.localTokenMint)
	if err != nil {
		return fmt.Errorf("failed to derive CCTP accounts: %w", err)
	}

	if err := s.validateUserTokenAccount(ctx, accounts.UserTokenAccount); err != nil {
		return fmt.Errorf("invalid user token account: %w", err)
	}

	instruction, err := s.buildReceiveMessageInstruction(msg.MsgSentBytes, attestationBytes, accounts)
	if err != nil {
		return fmt.Errorf("failed to build instruction: %w", err)
	}

	recent, err := s.rpcClient.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get recent blockhash: %w", err)
	}

	tx, err := solana.NewTransaction(
		[]solana.Instruction{instruction},
		recent.Value.Blockhash,
		solana.TransactionPayer(s.minterAddress),
	)
	if err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}

	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(s.minterAddress) {
			return &s.privateKey
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to sign transaction: %w", err)
	}

	sig, err := s.rpcClient.SendTransactionWithOpts(ctx, tx, rpc.TransactionOpts{
		SkipPreflight:       false,
		PreflightCommitment: rpc.CommitmentFinalized,
	})
	if err != nil {
		logger.Error(fmt.Sprintf("error during broadcast: %s", err.Error()))
		return err
	}

	msg.Status = types.Complete
	msg.DestTxHash = sig.String()

	logger.Info(fmt.Sprintf("Successfully broadcast %s to Solana. Tx signature: %s", msg.SourceTxHash, msg.DestTxHash))
	return nil
}

// validateUserTokenAccount verifies the mint recipient account exists
func (s *Solana) validateUserTokenAccount(ctx context.Context, userTokenAccount solana.PublicKey) error {
	accountInfo, err := s.rpcClient.GetAccountInfo(ctx, userTokenAccount)
	if err != nil {
		return fmt.Errorf("user token account does not exist: %w", err)
	}

	if accountInfo.Value == nil {
		return fmt.Errorf("user token account does not exist")
	}

	return nil
}

// buildReceiveMessageInstruction constructs the Solana CCTP receiveMessage instruction with required accounts
func (s *Solana) buildReceiveMessageInstruction(
	messageBytes []byte,
	attestationBytes []byte,
	accounts *CCTPAccounts,
) (solana.Instruction, error) {
	parsedMsg, err := new(types.Message).Parse(messageBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse message: %w", err)
	}

	recipient, err := BytesToSolanaPublicKey(parsedMsg.Recipient)
	if err != nil {
		return nil, fmt.Errorf("failed to parse recipient: %w", err)
	}

	instructionData := make([]byte, 0, len(messageBytes)+len(attestationBytes))
	instructionData = append(instructionData, messageBytes...)
	instructionData = append(instructionData, attestationBytes...)

	// Account order must match TokenMessengerMinter receiveMessage requirements
	accountMetas := solana.AccountMetaSlice{
		{PublicKey: accounts.MessageTransmitter, IsSigner: false, IsWritable: false},
		{PublicKey: s.minterAddress, IsSigner: true, IsWritable: false},
		{PublicKey: recipient, IsSigner: false, IsWritable: false},
		{PublicKey: accounts.UsedNonces, IsSigner: false, IsWritable: true},
		{PublicKey: accounts.TokenMessenger, IsSigner: false, IsWritable: false},
		{PublicKey: accounts.RemoteTokenMessenger, IsSigner: false, IsWritable: false},
		{PublicKey: accounts.TokenMinter, IsSigner: false, IsWritable: true},
		{PublicKey: accounts.LocalToken, IsSigner: false, IsWritable: true},
		{PublicKey: accounts.TokenPair, IsSigner: false, IsWritable: false},
		{PublicKey: accounts.UserTokenAccount, IsSigner: false, IsWritable: true},
		{PublicKey: accounts.CustodyTokenAccount, IsSigner: false, IsWritable: true},
		{PublicKey: accounts.TokenProgram, IsSigner: false, IsWritable: false},
		{PublicKey: accounts.EventAuthority, IsSigner: false, IsWritable: false},
		{PublicKey: accounts.TokenMessengerMinterProgram, IsSigner: false, IsWritable: false},
	}

	return solana.NewInstruction(s.messageTransmitterProgram, accountMetas, instructionData), nil
}
