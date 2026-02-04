package ipfs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/mtlprog/total/internal/config"
	"github.com/samber/hot"
)

// ErrInvalidCID is returned when an IPFS CID has invalid format.
var ErrInvalidCID = errors.New("invalid IPFS CID format")

// ipfsCIDPattern matches IPFS CIDv0 (Qm...) and CIDv1 (b...) formats.
var ipfsCIDPattern = regexp.MustCompile(`^(Qm[1-9A-HJ-NP-Za-km-z]{44}|b[A-Za-z2-7]{58,})$`)

const (
	// cacheTTL is the time-to-live for cached IPFS responses.
	cacheTTL = 5 * time.Minute
	// cacheSize is the maximum number of entries in the cache.
	cacheSize = 1000
)

// ValidateCID validates an IPFS CID format.
// Returns ErrInvalidCID if the CID is malformed.
func ValidateCID(cid string) error {
	if len(cid) < 10 || len(cid) > 100 {
		return ErrInvalidCID
	}
	if !ipfsCIDPattern.MatchString(cid) {
		return ErrInvalidCID
	}
	return nil
}

// Client provides IPFS operations via Pinata.
type Client struct {
	apiKey     string
	apiSecret  string
	gatewayURL string
	httpClient *http.Client
	cache      *hot.HotCache[string, []byte]
}

// NewClient creates a new IPFS client with caching.
func NewClient(apiKey, apiSecret string) *Client {
	c := &Client{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		gatewayURL: config.DefaultIPFSGateway,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	// Create cache with TTL and background revalidation.
	// Uses stale-while-revalidate pattern: serves cached data while refreshing in background.
	// On revalidation errors, stale data is preserved (KeepOnError policy).
	c.cache = hot.NewHotCache[string, []byte](hot.LRU, cacheSize).
		WithTTL(cacheTTL).
		WithRevalidation(cacheTTL, c.loadFromGateway).
		WithRevalidationErrorPolicy(hot.KeepOnError).
		Build()

	return c
}

// loadFromGateway is the cache loader that fetches data from IPFS gateway.
// Logs warnings for failed fetches but continues processing remaining hashes.
func (c *Client) loadFromGateway(hashes []string) (map[string][]byte, error) {
	result := make(map[string][]byte, len(hashes))
	var failedCount int

	for _, hash := range hashes {
		data, err := c.fetchFromGateway(context.Background(), hash)
		if err != nil {
			slog.Warn("cache revalidation fetch failed", "hash", hash, "error", err)
			failedCount++
			continue
		}
		result[hash] = data
	}

	if failedCount > 0 {
		slog.Warn("cache revalidation completed with errors",
			"total", len(hashes),
			"failed", failedCount,
			"succeeded", len(result))
	}

	return result, nil
}

// fetchFromGateway fetches raw JSON bytes from IPFS gateway.
// Validates CID format to prevent SSRF attacks.
func (c *Client) fetchFromGateway(ctx context.Context, hash string) ([]byte, error) {
	// Validate CID format to prevent SSRF
	if err := ValidateCID(hash); err != nil {
		return nil, fmt.Errorf("invalid IPFS hash %q: %w", hash, err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	url := c.gatewayURL + hash

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from IPFS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("IPFS error: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return data, nil
}

// PinataResponse is the response from Pinata pin API.
type PinataResponse struct {
	IpfsHash    string    `json:"IpfsHash"`
	PinSize     int       `json:"PinSize"`
	Timestamp   time.Time `json:"Timestamp"`
	IsDuplicate bool      `json:"isDuplicate"`
}

// PinJSON pins JSON data to IPFS via Pinata and returns the hash.
// Requires Pinata API credentials to be configured.
func (c *Client) PinJSON(ctx context.Context, data any) (string, error) {
	if c.apiKey == "" || c.apiSecret == "" {
		return "", fmt.Errorf("pinata credentials not configured")
	}

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

// GetJSON retrieves JSON data from IPFS by hash with caching.
// On cache miss, fetches from gateway and stores result for future requests.
func (c *Client) GetJSON(ctx context.Context, hash string, v any) error {
	// Try to get from cache (will trigger loader on miss)
	data, found, err := c.cache.Get(hash)
	if err != nil {
		return fmt.Errorf("cache error: %w", err)
	}

	if !found {
		// Cache miss and loader didn't find it, fetch directly
		data, err = c.fetchFromGateway(ctx, hash)
		if err != nil {
			return err
		}
		// Store in cache for future requests
		c.cache.Set(hash, data)
	}

	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("failed to decode JSON: %w", err)
	}

	return nil
}

// GatewayURL returns the IPFS gateway URL.
func (c *Client) GatewayURL() string {
	return c.gatewayURL
}

// CanPin returns true if Pinata credentials are configured for writing.
func (c *Client) CanPin() bool {
	return c.apiKey != "" && c.apiSecret != ""
}

// Warmup pre-fetches IPFS data for the given hashes to populate the cache.
// Runs in background goroutine and returns immediately. Empty hashes are skipped.
// Logs progress and errors. Note: goroutine may outlive caller.
func (c *Client) Warmup(hashes []string) {
	go func() {
		ctx := context.Background()
		var succeeded, failed int

		for _, hash := range hashes {
			if hash == "" {
				continue
			}
			data, err := c.fetchFromGateway(ctx, hash)
			if err != nil {
				slog.Warn("cache warmup fetch failed", "hash", hash, "error", err)
				failed++
				continue
			}
			c.cache.Set(hash, data)
			succeeded++
		}

		slog.Info("cache warmup completed", "succeeded", succeeded, "failed", failed)
	}()
}
