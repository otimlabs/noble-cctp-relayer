package filters

import (
	"context"
	"fmt"

	"cosmossdk.io/log"

	"github.com/strangelove-ventures/noble-cctp-relayer/types"
)

// RouteFilter validates messages against enabled CCTP routes
type RouteFilter struct {
	enabledRoutes map[types.Domain][]types.Domain
	logger        log.Logger
}

func NewRouteFilter() *RouteFilter {
	return &RouteFilter{}
}

func (f *RouteFilter) Name() string {
	return "route"
}

func (f *RouteFilter) Initialize(ctx context.Context, config map[string]interface{}, logger log.Logger) error {
	f.logger = logger
	routesRaw, ok := config["enabled_routes"]
	if !ok {
		return fmt.Errorf("route filter requires 'enabled_routes' in config")
	}
	routes, ok := routesRaw.(map[types.Domain][]types.Domain)
	if !ok {
		return fmt.Errorf("enabled_routes has invalid type")
	}
	f.enabledRoutes = routes
	logger.Info("Route filter initialized", "route_count", len(routes))
	return nil
}

func (f *RouteFilter) Filter(ctx context.Context, msg *types.MessageState) (bool, string, error) {
	destDomains, ok := f.enabledRoutes[msg.SourceDomain]
	if !ok {
		reason := fmt.Sprintf("route disabled: source_domain=%d dest_domain=%d (source not configured)",
			msg.SourceDomain, msg.DestDomain)
		return true, reason, nil
	}
	for _, dd := range destDomains {
		if dd == msg.DestDomain {
			return false, "", nil
		}
	}
	reason := fmt.Sprintf("route disabled: source_domain=%d dest_domain=%d (destination not in route)",
		msg.SourceDomain, msg.DestDomain)
	return true, reason, nil
}

func (f *RouteFilter) Close() error {
	return nil
}
