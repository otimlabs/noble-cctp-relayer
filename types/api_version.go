package types

import (
	"fmt"
	"strings"
)

// APIVersion represents the CCTP API version
type APIVersion int

const (
	APIVersionV1 APIVersion = iota + 1 // v1: /attestations/{messageHash}
	APIVersionV2                       // v2: /v2/messages/{sourceDomain}/{messageHash}
)

func (v APIVersion) String() string {
	switch v {
	case APIVersionV1:
		return "v1"
	case APIVersionV2:
		return "v2"
	default:
		return "v1"
	}
}

// ParseAPIVersion parses a string into APIVersion, returns error for invalid values
func ParseAPIVersion(s string) (APIVersion, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "v1", "1", "":
		return APIVersionV1, nil
	case "v2", "2":
		return APIVersionV2, nil
	default:
		return 0, fmt.Errorf("invalid API version %q: must be 'v1' or 'v2'", s)
	}
}
