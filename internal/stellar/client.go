package stellar

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/stellar/go-stellar-sdk/clients/horizonclient"
	"github.com/stellar/go-stellar-sdk/protocols/horizon"
	"github.com/stellar/go-stellar-sdk/protocols/horizon/operations"
	"github.com/stellar/go-stellar-sdk/txnbuild"
)

var (
	ErrAccountNotFound     = errors.New("stellar account not found")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrNetworkTimeout      = errors.New("stellar network timeout")
	ErrInvalidTransaction  = errors.New("invalid transaction")
)

// Client provides read operations for Stellar Horizon API.
type Client interface {
	// GetAccount returns account details from Horizon.
	GetAccount(ctx context.Context, publicKey string) (*horizon.Account, error)

	// GetAccountBalances returns all balances for an account.
	GetAccountBalances(ctx context.Context, publicKey string) ([]horizon.Balance, error)

	// GetAccountData returns data entries for an account.
	GetAccountData(ctx context.Context, publicKey string) (map[string]string, error)

	// GetTransactions returns recent transactions for an account.
	GetTransactions(ctx context.Context, publicKey string, limit int) ([]horizon.Transaction, error)

	// GetOperations returns recent operations for an account.
	GetOperations(ctx context.Context, publicKey string, limit int) ([]operations.Operation, error)

	// HorizonURL returns the Horizon server URL.
	HorizonURL() string

	// NetworkPassphrase returns the network passphrase.
	NetworkPassphrase() string
}

// HorizonClient implements Client using Stellar Horizon API.
type HorizonClient struct {
	client            *horizonclient.Client
	networkPassphrase string
}

// NewHorizonClient creates a new Horizon client.
func NewHorizonClient(horizonURL, networkPassphrase string) *HorizonClient {
	return &HorizonClient{
		client: &horizonclient.Client{
			HorizonURL: horizonURL,
			HTTP: &http.Client{
				Timeout: 30 * time.Second,
			},
		},
		networkPassphrase: networkPassphrase,
	}
}

// GetAccount implements Client.
func (c *HorizonClient) GetAccount(ctx context.Context, publicKey string) (*horizon.Account, error) {
	account, err := c.client.AccountDetail(horizonclient.AccountRequest{
		AccountID: publicKey,
	})
	if err != nil {
		if horizonclient.IsNotFoundError(err) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("failed to get account: %w", err)
	}
	return &account, nil
}

// GetAccountBalances implements Client.
func (c *HorizonClient) GetAccountBalances(ctx context.Context, publicKey string) ([]horizon.Balance, error) {
	account, err := c.GetAccount(ctx, publicKey)
	if err != nil {
		return nil, err
	}
	return account.Balances, nil
}

// GetAccountData implements Client.
func (c *HorizonClient) GetAccountData(ctx context.Context, publicKey string) (map[string]string, error) {
	account, err := c.GetAccount(ctx, publicKey)
	if err != nil {
		return nil, err
	}
	return account.Data, nil
}

// GetTransactions implements Client.
func (c *HorizonClient) GetTransactions(ctx context.Context, publicKey string, limit int) ([]horizon.Transaction, error) {
	request := horizonclient.TransactionRequest{
		ForAccount: publicKey,
		Limit:      uint(limit),
		Order:      horizonclient.OrderDesc,
	}

	page, err := c.client.Transactions(request)
	if err != nil {
		return nil, fmt.Errorf("failed to get transactions: %w", err)
	}

	return page.Embedded.Records, nil
}

// GetOperations implements Client.
func (c *HorizonClient) GetOperations(ctx context.Context, publicKey string, limit int) ([]operations.Operation, error) {
	request := horizonclient.OperationRequest{
		ForAccount: publicKey,
		Limit:      uint(limit),
		Order:      horizonclient.OrderDesc,
	}

	page, err := c.client.Operations(request)
	if err != nil {
		return nil, fmt.Errorf("failed to get operations: %w", err)
	}

	return page.Embedded.Records, nil
}

// HorizonURL implements Client.
func (c *HorizonClient) HorizonURL() string {
	return c.client.HorizonURL
}

// NetworkPassphrase implements Client.
func (c *HorizonClient) NetworkPassphrase() string {
	return c.networkPassphrase
}

// SubmitTransaction submits a signed transaction to the network.
func (c *HorizonClient) SubmitTransaction(tx *txnbuild.Transaction) (*horizon.Transaction, error) {
	resp, err := c.client.SubmitTransaction(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to submit transaction: %w", err)
	}
	return &resp, nil
}
