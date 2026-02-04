package filters

import (
	"context"
	"fmt"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// DestinationCallerFilter validates destination caller addresses
type DestinationCallerFilter struct {
	registeredDomains     map[types.Domain]types.Chain
	destinationCallerOnly bool
	logger                log.Logger
}

func NewDestinationCallerFilter() *DestinationCallerFilter {
	return &DestinationCallerFilter{}
}

func (f *DestinationCallerFilter) Name() string {
	return "destination-caller"
}

func (f *DestinationCallerFilter) Initialize(ctx context.Context, config map[string]interface{}, logger log.Logger) error {
	f.logger = logger
	domainsRaw, ok := config["registered_domains"]
	if !ok {
		return fmt.Errorf("destination-caller filter requires 'registered_domains' in config")
	}
	domains, ok := domainsRaw.(map[types.Domain]types.Chain)
	if !ok {
		return fmt.Errorf("registered_domains has invalid type")
	}
	f.registeredDomains = domains

	destCallerOnlyRaw, ok := config["destination_caller_only"]
	if !ok {
		f.destinationCallerOnly = false
	} else {
		destCallerOnly, ok := destCallerOnlyRaw.(bool)
		if !ok {
			return fmt.Errorf("destination_caller_only has invalid type")
		}
		f.destinationCallerOnly = destCallerOnly
	}

	mode := "permissionless"
	if f.destinationCallerOnly {
		mode = "destination-caller-only"
	}
	logger.Info("Destination caller filter initialized", "mode", mode)
	return nil
}

func (f *DestinationCallerFilter) Filter(ctx context.Context, msg *types.MessageState) (bool, string, error) {
	chain, ok := f.registeredDomains[msg.DestDomain]
	if !ok {
		reason := fmt.Sprintf("destination caller check failed: no chain registered for dest_domain=%d", msg.DestDomain)
		return true, reason, nil
	}

	validCaller, address := chain.IsDestinationCaller(msg.DestinationCaller)
	if validCaller {
		return false, "", nil
	}

	shouldFilter := f.destinationCallerOnly || address != ""
	if shouldFilter {
		reason := fmt.Sprintf("destination caller mismatch: source_domain=%d dest_domain=%d caller=%s",
			msg.SourceDomain, msg.DestDomain, address)
		return true, reason, nil
	}
	return false, "", nil
}

func (f *DestinationCallerFilter) Close() error {
	return nil
}
