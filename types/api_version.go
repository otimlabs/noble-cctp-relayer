package types

import (
	"fmt"
	"strings"
)

// APIVersion represents the CCTP API version
type APIVersion string

const (
	APIVersionV1 APIVersion = "v1" // v1: /attestations/{messageHash}
	APIVersionV2 APIVersion = "v2" // v2: /v2/messages/{sourceDomain}/{messageHash}
)

// ParseAPIVersion parses a string into APIVersion, returns error for invalid values
func ParseAPIVersion(s string) (APIVersion, error) {
	normalized := strings.ToLower(strings.TrimSpace(s))
	switch normalized {
	case "v1", "":
		return APIVersionV1, nil
	case "v2":
		return APIVersionV2, nil
	default:
		return "", fmt.Errorf("invalid API version %q: must be 'v1' or 'v2'", s)
	}
}
