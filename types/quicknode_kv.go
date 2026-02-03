package types

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	quickNodeBaseURL = "https://api.quicknode.com/kv/rest/v1/lists"
	requestTimeout   = 10 * time.Second
)

// QuickNodeKVClient handles interactions with QuickNode's KV store REST API
type QuickNodeKVClient struct {
	apiKey     string
	httpClient *http.Client
}

// QuickNodeKVResponse represents the response from QuickNode KV store
type QuickNodeKVResponse struct {
	Data struct {
		Items []string `json:"items"`
	} `json:"data"`
}

// NewQuickNodeKVClient creates a new QuickNode KV client
func NewQuickNodeKVClient(apiKey string) *QuickNodeKVClient {
	return &QuickNodeKVClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

// FetchList retrieves a list of items from the QuickNode KV store
func (c *QuickNodeKVClient) FetchList(key string) ([]string, error) {
	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	url := fmt.Sprintf("%s/%s", quickNodeBaseURL, key)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var kvResponse QuickNodeKVResponse
	if err := json.Unmarshal(body, &kvResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return kvResponse.Data.Items, nil
}
