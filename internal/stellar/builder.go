package stellar

import (
	"context"
	"fmt"

	"github.com/mtlprog/total/internal/soroban"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// Builder creates Soroban transactions for market operations.
type Builder struct {
	client            Client
	networkPassphrase string
	baseFee           int64
	sorobanClient     *soroban.Client
	contractInvoker   *soroban.ContractInvoker
}

// NewBuilder creates a new transaction builder.
func NewBuilder(client Client, networkPassphrase string, baseFee int64, sorobanClient *soroban.Client) *Builder {
	b := &Builder{
		client:            client,
		networkPassphrase: networkPassphrase,
		baseFee:           baseFee,
		sorobanClient:     sorobanClient,
	}
	if sorobanClient != nil {
		b.contractInvoker = soroban.NewContractInvoker(sorobanClient, networkPassphrase, baseFee)
	}
	return b
}

// BuyTxParams contains parameters for buying tokens via Soroban contract.
type BuyTxParams struct {
	UserPublicKey string
	ContractID    string
	Outcome       uint32 // 0 for YES, 1 for NO
	Amount        int64  // Amount scaled by 10^7
	MaxCost       int64  // Max cost scaled by 10^7
}

// BuildBuyTx builds an InvokeHostFunction transaction for buying tokens.
func (b *Builder) BuildBuyTx(ctx context.Context, params BuyTxParams) (string, error) {
	if b.contractInvoker == nil {
		return "", fmt.Errorf("soroban client not configured")
	}

	userAccount, err := b.client.GetAccount(ctx, params.UserPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get user account: %w", err)
	}

	userAddr, err := soroban.EncodeAddress(params.UserPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to encode user address: %w", err)
	}

	args := []xdr.ScVal{
		userAddr,
		soroban.EncodeU32(params.Outcome),
		soroban.EncodeI128(params.Amount),
		soroban.EncodeI128(params.MaxCost),
	}

	invokeParams := soroban.InvokeParams{
		SourceAccount: userAccount,
		ContractID:    params.ContractID,
		FunctionName:  "buy",
		Args:          args,
	}

	return b.contractInvoker.BuildInvokeTx(ctx, invokeParams)
}

// SellTxParams contains parameters for selling tokens via Soroban contract.
type SellTxParams struct {
	UserPublicKey string
	ContractID    string
	Outcome       uint32 // 0 for YES, 1 for NO
	Amount        int64  // Amount scaled by 10^7
	MinReturn     int64  // Min return scaled by 10^7
}

// BuildSellTx builds an InvokeHostFunction transaction for selling tokens.
func (b *Builder) BuildSellTx(ctx context.Context, params SellTxParams) (string, error) {
	if b.contractInvoker == nil {
		return "", fmt.Errorf("soroban client not configured")
	}

	userAccount, err := b.client.GetAccount(ctx, params.UserPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get user account: %w", err)
	}

	userAddr, err := soroban.EncodeAddress(params.UserPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to encode user address: %w", err)
	}

	args := []xdr.ScVal{
		userAddr,
		soroban.EncodeU32(params.Outcome),
		soroban.EncodeI128(params.Amount),
		soroban.EncodeI128(params.MinReturn),
	}

	invokeParams := soroban.InvokeParams{
		SourceAccount: userAccount,
		ContractID:    params.ContractID,
		FunctionName:  "sell",
		Args:          args,
	}

	return b.contractInvoker.BuildInvokeTx(ctx, invokeParams)
}

// ResolveTxParams contains parameters for resolving a market via Soroban contract.
type ResolveTxParams struct {
	OraclePublicKey string
	ContractID      string
	WinningOutcome  uint32 // 0 for YES, 1 for NO
}

// BuildResolveTx builds an InvokeHostFunction transaction to resolve a market.
func (b *Builder) BuildResolveTx(ctx context.Context, params ResolveTxParams) (string, error) {
	if b.contractInvoker == nil {
		return "", fmt.Errorf("soroban client not configured")
	}

	oracleAccount, err := b.client.GetAccount(ctx, params.OraclePublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get oracle account: %w", err)
	}

	oracleAddr, err := soroban.EncodeAddress(params.OraclePublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to encode oracle address: %w", err)
	}

	args := []xdr.ScVal{
		oracleAddr,
		soroban.EncodeU32(params.WinningOutcome),
	}

	invokeParams := soroban.InvokeParams{
		SourceAccount: oracleAccount,
		ContractID:    params.ContractID,
		FunctionName:  "resolve",
		Args:          args,
	}

	return b.contractInvoker.BuildInvokeTx(ctx, invokeParams)
}

// ClaimTxParams contains parameters for claiming winnings via Soroban contract.
type ClaimTxParams struct {
	UserPublicKey string
	ContractID    string
}

// BuildClaimTx builds an InvokeHostFunction transaction to claim winnings.
func (b *Builder) BuildClaimTx(ctx context.Context, params ClaimTxParams) (string, error) {
	if b.contractInvoker == nil {
		return "", fmt.Errorf("soroban client not configured")
	}

	userAccount, err := b.client.GetAccount(ctx, params.UserPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get user account: %w", err)
	}

	userAddr, err := soroban.EncodeAddress(params.UserPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to encode user address: %w", err)
	}

	args := []xdr.ScVal{
		userAddr,
	}

	invokeParams := soroban.InvokeParams{
		SourceAccount: userAccount,
		ContractID:    params.ContractID,
		FunctionName:  "claim",
		Args:          args,
	}

	return b.contractInvoker.BuildInvokeTx(ctx, invokeParams)
}

// WithdrawTxParams contains parameters for oracle withdrawing remaining pool.
type WithdrawTxParams struct {
	OraclePublicKey string
	ContractID      string
}

// BuildWithdrawTx builds an InvokeHostFunction transaction for withdrawing remaining pool.
func (b *Builder) BuildWithdrawTx(ctx context.Context, params WithdrawTxParams) (string, error) {
	if b.contractInvoker == nil {
		return "", fmt.Errorf("soroban client not configured")
	}

	oracleAccount, err := b.client.GetAccount(ctx, params.OraclePublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get oracle account: %w", err)
	}

	oracleAddr, err := soroban.EncodeAddress(params.OraclePublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to encode oracle address: %w", err)
	}

	args := []xdr.ScVal{oracleAddr}

	invokeParams := soroban.InvokeParams{
		SourceAccount: oracleAccount,
		ContractID:    params.ContractID,
		FunctionName:  "withdraw_remaining",
		Args:          args,
	}

	return b.contractInvoker.BuildInvokeTx(ctx, invokeParams)
}

// GetQuoteTxParams contains parameters for getting a price quote.
type GetQuoteTxParams struct {
	UserPublicKey string
	ContractID    string
	Outcome       uint32 // 0 for YES, 1 for NO
	Amount        int64  // Amount scaled by 10^7
}

// BuildGetQuoteTx builds a transaction to get a price quote (simulation only).
func (b *Builder) BuildGetQuoteTx(ctx context.Context, params GetQuoteTxParams) (string, error) {
	if b.contractInvoker == nil {
		return "", fmt.Errorf("soroban client not configured")
	}

	userAccount, err := b.client.GetAccount(ctx, params.UserPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get user account: %w", err)
	}

	args := []xdr.ScVal{
		soroban.EncodeU32(params.Outcome),
		soroban.EncodeI128(params.Amount),
	}

	invokeParams := soroban.InvokeParams{
		SourceAccount: userAccount,
		ContractID:    params.ContractID,
		FunctionName:  "get_quote",
		Args:          args,
	}

	return b.contractInvoker.BuildInvokeTx(ctx, invokeParams)
}

// BuildGetSellQuoteTx builds a transaction to get a sell quote (simulation only).
func (b *Builder) BuildGetSellQuoteTx(ctx context.Context, params GetQuoteTxParams) (string, error) {
	if b.contractInvoker == nil {
		return "", fmt.Errorf("soroban client not configured")
	}

	userAccount, err := b.client.GetAccount(ctx, params.UserPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get user account: %w", err)
	}

	args := []xdr.ScVal{
		soroban.EncodeU32(params.Outcome),
		soroban.EncodeI128(params.Amount),
	}

	invokeParams := soroban.InvokeParams{
		SourceAccount: userAccount,
		ContractID:    params.ContractID,
		FunctionName:  "get_sell_quote",
		Args:          args,
	}

	return b.contractInvoker.BuildInvokeTx(ctx, invokeParams)
}

// SimulateAndPrepareTx simulates a Soroban transaction and returns it with resources attached.
func (b *Builder) SimulateAndPrepareTx(ctx context.Context, txXDR string) (string, error) {
	if b.contractInvoker == nil {
		return "", fmt.Errorf("soroban client not configured")
	}
	return b.contractInvoker.SimulateAndPrepare(ctx, txXDR)
}

// --- Factory contract methods ---

// ListMarketsTxParams contains parameters for listing markets from factory.
type ListMarketsTxParams struct {
	UserPublicKey   string
	FactoryContract string
}

// BuildListMarketsTx builds a transaction to call factory.list_markets() (simulation only).
func (b *Builder) BuildListMarketsTx(ctx context.Context, params ListMarketsTxParams) (string, error) {
	if b.contractInvoker == nil {
		return "", fmt.Errorf("soroban client not configured")
	}

	userAccount, err := b.client.GetAccount(ctx, params.UserPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get user account: %w", err)
	}

	// list_markets() takes no arguments
	invokeParams := soroban.InvokeParams{
		SourceAccount: userAccount,
		ContractID:    params.FactoryContract,
		FunctionName:  "list_markets",
		Args:          []xdr.ScVal{},
	}

	return b.contractInvoker.BuildInvokeTx(ctx, invokeParams)
}

// GetStateTxParams contains parameters for getting market state.
type GetStateTxParams struct {
	UserPublicKey string
	ContractID    string
}

// BuildGetStateTx builds a transaction to call market.get_state() (simulation only).
func (b *Builder) BuildGetStateTx(ctx context.Context, params GetStateTxParams) (string, error) {
	if b.contractInvoker == nil {
		return "", fmt.Errorf("soroban client not configured")
	}

	userAccount, err := b.client.GetAccount(ctx, params.UserPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get user account: %w", err)
	}

	// get_state() takes no arguments
	invokeParams := soroban.InvokeParams{
		SourceAccount: userAccount,
		ContractID:    params.ContractID,
		FunctionName:  "get_state",
		Args:          []xdr.ScVal{},
	}

	return b.contractInvoker.BuildInvokeTx(ctx, invokeParams)
}

// GetMetadataHashTxParams contains parameters for getting metadata hash.
type GetMetadataHashTxParams struct {
	UserPublicKey string
	ContractID    string
}

// BuildGetMetadataHashTx builds a transaction to call market.get_metadata_hash() (simulation only).
func (b *Builder) BuildGetMetadataHashTx(ctx context.Context, params GetMetadataHashTxParams) (string, error) {
	if b.contractInvoker == nil {
		return "", fmt.Errorf("soroban client not configured")
	}

	userAccount, err := b.client.GetAccount(ctx, params.UserPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get user account: %w", err)
	}

	// get_metadata_hash() takes no arguments
	invokeParams := soroban.InvokeParams{
		SourceAccount: userAccount,
		ContractID:    params.ContractID,
		FunctionName:  "get_metadata_hash",
		Args:          []xdr.ScVal{},
	}

	return b.contractInvoker.BuildInvokeTx(ctx, invokeParams)
}

// DeployMarketTxParams contains parameters for deploying a new market via factory.
type DeployMarketTxParams struct {
	OraclePublicKey string
	FactoryContract string
	LiquidityParam  int64 // Scaled by 10^7
	MetadataHash    string
	InitialFunding  int64 // Scaled by 10^7
	Salt            [32]byte
}

// BuildDeployMarketTx builds a transaction to call factory.deploy_market().
func (b *Builder) BuildDeployMarketTx(ctx context.Context, params DeployMarketTxParams) (string, error) {
	if b.contractInvoker == nil {
		return "", fmt.Errorf("soroban client not configured")
	}

	oracleAccount, err := b.client.GetAccount(ctx, params.OraclePublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get oracle account: %w", err)
	}

	oracleAddr, err := soroban.EncodeAddress(params.OraclePublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to encode oracle address: %w", err)
	}

	// deploy_market(oracle, liquidity_param, metadata_hash, initial_funding, salt)
	args := []xdr.ScVal{
		oracleAddr,
		soroban.EncodeI128(params.LiquidityParam),
		soroban.EncodeString(params.MetadataHash),
		soroban.EncodeI128(params.InitialFunding),
		soroban.EncodeBytes32(params.Salt),
	}

	invokeParams := soroban.InvokeParams{
		SourceAccount: oracleAccount,
		ContractID:    params.FactoryContract,
		FunctionName:  "deploy_market",
		Args:          args,
	}

	return b.contractInvoker.BuildInvokeTx(ctx, invokeParams)
}
