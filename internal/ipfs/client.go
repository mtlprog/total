package ipfs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mtlprog/total/internal/config"
)

// Client provides IPFS operations via Pinata.
type Client struct {
	apiKey     string
	apiSecret  string
	gatewayURL string
	httpClient *http.Client
}

// NewClient creates a new IPFS client.
func NewClient(apiKey, apiSecret string) *Client {
	return &Client{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		gatewayURL: config.DefaultIPFSGateway,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// PinataResponse is the response from Pinata pin API.
type PinataResponse struct {
	IpfsHash    string    `json:"IpfsHash"`
	PinSize     int       `json:"PinSize"`
	Timestamp   time.Time `json:"Timestamp"`
	IsDuplicate bool      `json:"isDuplicate"`
}

// PinJSON pins JSON data to IPFS via Pinata and returns the hash.
func (c *Client) PinJSON(ctx context.Context, data any) (string, error) {
	jsonData, err := json.Marshal(map[string]any{
		"pinataContent": data,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", config.PinataAPIURL, bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("pinata_api_key", c.apiKey)
	req.Header.Set("pinata_secret_api_key", c.apiSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to pin JSON: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("pinata error: %s - %s", resp.Status, string(body))
	}

	var pinataResp PinataResponse
	if err := json.NewDecoder(resp.Body).Decode(&pinataResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return pinataResp.IpfsHash, nil
}

// GetJSON retrieves JSON data from IPFS by hash.
func (c *Client) GetJSON(ctx context.Context, hash string, v any) error {
	url := c.gatewayURL + hash

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch from IPFS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("IPFS error: %s", resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("failed to decode JSON: %w", err)
	}

	return nil
}

// GatewayURL returns the IPFS gateway URL.
func (c *Client) GatewayURL() string {
	return c.gatewayURL
}
