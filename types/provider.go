package types

import "context"

// DataProvider abstracts data sources for filters
type DataProvider interface {
	Name() string
	FetchList(ctx context.Context, key string) ([]string, error)
	Initialize(config map[string]interface{}) error
	Close() error
}
