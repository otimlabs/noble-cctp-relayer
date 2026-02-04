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

type QuickNodeKVProvider struct {
	apiKey     string
	httpClient *http.Client
}

type QuickNodeKVResponse struct {
	Data struct {
		Items []string `json:"items"`
	} `json:"data"`
}

func NewQuickNodeKVProvider() *QuickNodeKVProvider {
	return &QuickNodeKVProvider{
		httpClient: &http.Client{Timeout: requestTimeout},
	}
}

func (p *QuickNodeKVProvider) Name() string {
	return "quicknode-kv"
}

func (p *QuickNodeKVProvider) Initialize(config map[string]interface{}) error {
	apiKey, ok := config["api_key"].(string)
	if !ok || apiKey == "" {
		return fmt.Errorf("quicknode-kv provider requires 'api_key' in config")
	}
	p.apiKey = apiKey
	return nil
}

// FetchList retrieves a list of items from the QuickNode KV store
func (p *QuickNodeKVProvider) FetchList(ctx context.Context, key string) ([]string, error) {
	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	url := fmt.Sprintf("%s/%s", quickNodeBaseURL, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)

	resp, err := p.httpClient.Do(req)
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

func (p *QuickNodeKVProvider) Close() error {
	p.httpClient.CloseIdleConnections()
	return nil
}
