package types

import (
	"context"

	"cosmossdk.io/log"
)

// MessageFilter defines the interface for message filtering plugins
type MessageFilter interface {
	Name() string
	Filter(ctx context.Context, msg *MessageState) (shouldFilter bool, reason string, err error)
	Initialize(ctx context.Context, config map[string]interface{}, logger log.Logger) error
	Close() error
}

// FilterRegistry manages message filters
type FilterRegistry struct {
	filters []MessageFilter
	logger  log.Logger
}

// NewFilterRegistry creates a new filter registry
func NewFilterRegistry(logger log.Logger) *FilterRegistry {
	return &FilterRegistry{
		filters: make([]MessageFilter, 0),
		logger:  logger,
	}
}

func (r *FilterRegistry) Register(filter MessageFilter) {
	r.filters = append(r.filters, filter)
	r.logger.Debug("Registered filter", "name", filter.Name())
}

func (r *FilterRegistry) Filter(ctx context.Context, msg *MessageState) (shouldFilter bool, reason string) {
	for _, filter := range r.filters {
		filtered, filterReason, err := filter.Filter(ctx, msg)
		if err != nil {
			r.logger.Error("Filter error", "filter", filter.Name(), "error", err)
			continue
		}
		if filtered {
			return true, filterReason
		}
	}
	return false, ""
}

func (r *FilterRegistry) Close() error {
	for _, filter := range r.filters {
		if err := filter.Close(); err != nil {
			r.logger.Error("Error closing filter", "filter", filter.Name(), "error", err)
		}
	}
	return nil
}
