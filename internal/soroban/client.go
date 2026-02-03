package soroban

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var (
	ErrRPCError            = errors.New("RPC error")
	ErrSimulationFailed    = errors.New("simulation failed")
	ErrTransactionFailed   = errors.New("transaction failed")
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrTimeout             = errors.New("timeout waiting for transaction")
)

// Client is a Soroban RPC client.
type Client struct {
	rpcURL     string
	httpClient *http.Client
	requestID  int
}

// NewClient creates a new Soroban RPC client.
func NewClient(rpcURL string) *Client {
	return &Client{
		rpcURL: rpcURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		requestID: 1,
	}
}

// RPCURL returns the RPC URL.
func (c *Client) RPCURL() string {
	return c.rpcURL
}

// call makes a JSON-RPC call.
func (c *Client) call(ctx context.Context, method string, params any) (*RPCResponse, error) {
	c.requestID++

	req := RPCRequest{
		JSONRPC: "2.0",
		ID:      c.requestID,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp RPCResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%w: %s", ErrRPCError, resp.Error.Error())
	}

	return &resp, nil
}

// GetHealth checks the health of the RPC server.
func (c *Client) GetHealth(ctx context.Context) (*GetHealthResult, error) {
	resp, err := c.call(ctx, "getHealth", nil)
	if err != nil {
		return nil, err
	}

	var result GetHealthResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	return &result, nil
}

// GetNetwork gets network information.
func (c *Client) GetNetwork(ctx context.Context) (*GetNetworkResult, error) {
	resp, err := c.call(ctx, "getNetwork", nil)
	if err != nil {
		return nil, err
	}

	var result GetNetworkResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	return &result, nil
}

// GetLatestLedger gets the latest ledger info.
func (c *Client) GetLatestLedger(ctx context.Context) (*GetLatestLedgerResult, error) {
	resp, err := c.call(ctx, "getLatestLedger", nil)
	if err != nil {
		return nil, err
	}

	var result GetLatestLedgerResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	return &result, nil
}

// SimulateTransaction simulates a transaction to get resource requirements.
func (c *Client) SimulateTransaction(ctx context.Context, txXDR string) (*SimulateTransactionResult, error) {
	params := SimulateTransactionParams{
		Transaction: txXDR,
	}

	resp, err := c.call(ctx, "simulateTransaction", params)
	if err != nil {
		return nil, err
	}

	var result SimulateTransactionResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	if result.Error != "" {
		return &result, fmt.Errorf("%w: %s", ErrSimulationFailed, result.Error)
	}

	return &result, nil
}

// SendTransaction submits a signed transaction.
func (c *Client) SendTransaction(ctx context.Context, txXDR string) (*SendTransactionResult, error) {
	params := SendTransactionParams{
		Transaction: txXDR,
	}

	resp, err := c.call(ctx, "sendTransaction", params)
	if err != nil {
		return nil, err
	}

	var result SendTransactionResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	if result.Status == TxStatusError {
		return &result, fmt.Errorf("%w: %s", ErrTransactionFailed, result.ErrorResult)
	}

	return &result, nil
}

// GetTransaction gets the status and result of a transaction.
func (c *Client) GetTransaction(ctx context.Context, hash string) (*GetTransactionResult, error) {
	params := GetTransactionParams{
		Hash: hash,
	}

	resp, err := c.call(ctx, "getTransaction", params)
	if err != nil {
		return nil, err
	}

	var result GetTransactionResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	return &result, nil
}

// WaitForTransaction polls until transaction completes or timeout.
func (c *Client) WaitForTransaction(ctx context.Context, hash string, timeout time.Duration) (*GetTransactionResult, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("%w: %s", ErrTimeout, hash)
			}

			result, err := c.GetTransaction(ctx, hash)
			if err != nil {
				return nil, err
			}

			switch result.Status {
			case TxResultNotFound:
				continue
			case TxResultSuccess:
				return result, nil
			case TxResultFailed:
				return result, fmt.Errorf("%w: %s", ErrTransactionFailed, result.ResultXdr)
			default:
				continue
			}
		}
	}
}

// GetLedgerEntries retrieves ledger entries by their keys.
func (c *Client) GetLedgerEntries(ctx context.Context, keys []string) (*GetLedgerEntriesResult, error) {
	params := GetLedgerEntriesParams{
		Keys: keys,
	}

	resp, err := c.call(ctx, "getLedgerEntries", params)
	if err != nil {
		return nil, err
	}

	var result GetLedgerEntriesResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	return &result, nil
}
