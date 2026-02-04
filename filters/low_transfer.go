package filters

import (
	"context"
	"fmt"

	cctptypes "github.com/circlefin/noble-cctp/x/cctp/types"

	"cosmossdk.io/log"
	"cosmossdk.io/math"

	"github.com/strangelove-ventures/noble-cctp-relayer/ethereum"
	"github.com/strangelove-ventures/noble-cctp-relayer/noble"
	"github.com/strangelove-ventures/noble-cctp-relayer/solana"
	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// LowTransferFilter filters transfers below minimum mint amounts
type LowTransferFilter struct {
	chains map[string]types.ChainConfig
	logger log.Logger
}

func NewLowTransferFilter() *LowTransferFilter {
	return &LowTransferFilter{}
}

func (f *LowTransferFilter) Name() string {
	return "low-transfer"
}

func (f *LowTransferFilter) Initialize(ctx context.Context, config map[string]interface{}, logger log.Logger) error {
	f.logger = logger
	chainsRaw, ok := config["chains"]
	if !ok {
		return fmt.Errorf("low-transfer filter requires 'chains' in config")
	}
	chains, ok := chainsRaw.(map[string]types.ChainConfig)
	if !ok {
		return fmt.Errorf("chains has invalid type")
	}
	f.chains = chains
	logger.Info("Low transfer filter initialized", "chain_count", len(chains))
	return nil
}

func (f *LowTransferFilter) Filter(ctx context.Context, msg *types.MessageState) (bool, string, error) {
	bm, err := new(cctptypes.BurnMessage).Parse(msg.MsgBody)
	if err != nil {
		reason := fmt.Sprintf("not a valid burn message: %v", err)
		return true, reason, nil
	}

	minBurnAmount := f.getMinMintAmount(msg.DestDomain)
	if minBurnAmount == 0 {
		return false, "", nil
	}

	if bm.Amount.LT(math.NewIntFromUint64(minBurnAmount)) {
		reason := fmt.Sprintf("transfer amount too low: amount=%s min_amount=%d dest_domain=%d",
			bm.Amount.String(), minBurnAmount, msg.DestDomain)
		return true, reason, nil
	}

	return false, "", nil
}

// Close cleans up filter resources
func (f *LowTransferFilter) Close() error {
	return nil
}

func (f *LowTransferFilter) getMinMintAmount(destDomain types.Domain) uint64 {
	if destDomain == types.Domain(4) {
		nobleCfg, ok := f.chains["noble"].(*noble.ChainConfig)
		if !ok {
			f.logger.Info("Chain named 'noble' not found in config")
			return 0
		}
		return nobleCfg.MinMintAmount
	}

	for _, chain := range f.chains {
		switch c := chain.(type) {
		case *ethereum.ChainConfig:
			if c.Domain == destDomain {
				return c.MinMintAmount
			}
		case *solana.ChainConfig:
			if c.Domain == destDomain {
				return c.MinMintAmount
			}
		}
	}
	return 0
}
