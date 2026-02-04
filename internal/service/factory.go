package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/mtlprog/total/internal/model"
	"github.com/mtlprog/total/internal/soroban"
	"github.com/mtlprog/total/internal/stellar"
)

var (
	ErrFactoryNotConfigured = errors.New("factory contract not configured")
	ErrInvalidMetadataHash  = errors.New("invalid metadata hash")
)

// FactoryService handles market factory operations.
type FactoryService struct {
	sorobanClient   *soroban.Client
	stellarClient   stellar.Client
	txBuilder       *stellar.Builder
	factoryContract string
	oraclePublicKey string
	logger          *slog.Logger
}

// NewFactoryService creates a new factory service.
func NewFactoryService(
	sorobanClient *soroban.Client,
	stellarClient stellar.Client,
	txBuilder *stellar.Builder,
	factoryContract string,
	oraclePublicKey string,
	logger *slog.Logger,
) *FactoryService {
	return &FactoryService{
		sorobanClient:   sorobanClient,
		stellarClient:   stellarClient,
		txBuilder:       txBuilder,
		factoryContract: factoryContract,
		oraclePublicKey: oraclePublicKey,
		logger:          logger,
	}
}

// HasFactory returns true if factory contract is configured.
func (s *FactoryService) HasFactory() bool {
	return s.factoryContract != ""
}

// FactoryContractID returns the factory contract ID.
func (s *FactoryService) FactoryContractID() string {
	return s.factoryContract
}

// ListMarkets returns all market contract IDs from the factory.
func (s *FactoryService) ListMarkets(ctx context.Context) ([]string, error) {
	if s.factoryContract == "" {
		return nil, ErrFactoryNotConfigured
	}

	txXDR, err := s.txBuilder.BuildListMarketsTx(ctx, stellar.ListMarketsTxParams{
		UserPublicKey:   s.oraclePublicKey,
		FactoryContract: s.factoryContract,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build list_markets tx: %w", err)
	}

	simResult, err := s.sorobanClient.SimulateTransaction(ctx, txXDR)
	if err != nil {
		return nil, fmt.Errorf("failed to simulate list_markets: %w", err)
	}

	if simResult.Error != "" {
		return nil, fmt.Errorf("simulation error: %s", simResult.Error)
	}

	if len(simResult.Results) == 0 || simResult.Results[0].XDR == "" {
		return nil, fmt.Errorf("no result from simulation")
	}

	returnVal, err := soroban.ParseReturnValue(simResult.Results[0].XDR)
	if err != nil {
		return nil, fmt.Errorf("failed to parse return value: %w", err)
	}

	// list_markets returns Vec<Address>
	addresses, err := soroban.DecodeVec(returnVal)
	if err != nil {
		return nil, fmt.Errorf("failed to decode addresses: %w", err)
	}

	contractIDs := make([]string, 0, len(addresses))
	for _, addr := range addresses {
		contractID, err := soroban.DecodeAddress(addr)
		if err != nil {
			s.logger.Warn("failed to decode market address", "error", err)
			continue
		}
		contractIDs = append(contractIDs, contractID)
	}

	return contractIDs, nil
}

// MarketState represents the current state of a market contract.
type MarketState struct {
	ContractID   string
	YesSold      int64
	NoSold       int64
	Pool         int64
	Resolved     bool
	MetadataHash string
	PriceYes     float64
	PriceNo      float64
}

// GetMarketStates fetches state for multiple markets in parallel.
func (s *FactoryService) GetMarketStates(ctx context.Context, contractIDs []string) ([]MarketState, error) {
	states := make([]MarketState, len(contractIDs))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for i, id := range contractIDs {
		wg.Add(1)
		go func(idx int, contractID string) {
			defer wg.Done()

			state, err := s.getMarketState(ctx, contractID)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("failed to get state for %s: %w", contractID, err)
				}
				mu.Unlock()
				s.logger.Warn("failed to get market state", "contract_id", contractID, "error", err)
				return
			}

			mu.Lock()
			states[idx] = *state
			mu.Unlock()
		}(i, id)
	}

	wg.Wait()

	// Filter out empty states (failed fetches)
	validStates := make([]MarketState, 0, len(states))
	for _, state := range states {
		if state.ContractID != "" {
			validStates = append(validStates, state)
		}
	}

	return validStates, nil
}

// getMarketState fetches state for a single market.
func (s *FactoryService) getMarketState(ctx context.Context, contractID string) (*MarketState, error) {
	// Get state (yes_sold, no_sold, pool, resolved)
	stateTxXDR, err := s.txBuilder.BuildGetStateTx(ctx, stellar.GetStateTxParams{
		UserPublicKey: s.oraclePublicKey,
		ContractID:    contractID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build get_state tx: %w", err)
	}

	simResult, err := s.sorobanClient.SimulateTransaction(ctx, stateTxXDR)
	if err != nil {
		return nil, fmt.Errorf("failed to simulate get_state: %w", err)
	}

	if simResult.Error != "" {
		return nil, fmt.Errorf("simulation error: %s", simResult.Error)
	}

	if len(simResult.Results) == 0 || simResult.Results[0].XDR == "" {
		return nil, fmt.Errorf("no result from simulation")
	}

	returnVal, err := soroban.ParseReturnValue(simResult.Results[0].XDR)
	if err != nil {
		return nil, fmt.Errorf("failed to parse return value: %w", err)
	}

	// get_state returns (yes_sold, no_sold, pool, resolved)
	tuple, err := soroban.DecodeVec(returnVal)
	if err != nil {
		return nil, fmt.Errorf("failed to decode state tuple: %w", err)
	}

	if len(tuple) < 4 {
		return nil, fmt.Errorf("expected 4 elements in state tuple, got %d", len(tuple))
	}

	yesSold, err := soroban.DecodeI128(tuple[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode yes_sold: %w", err)
	}

	noSold, err := soroban.DecodeI128(tuple[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode no_sold: %w", err)
	}

	pool, err := soroban.DecodeI128(tuple[2])
	if err != nil {
		return nil, fmt.Errorf("failed to decode pool: %w", err)
	}

	resolved, err := soroban.DecodeBool(tuple[3])
	if err != nil {
		return nil, fmt.Errorf("failed to decode resolved: %w", err)
	}

	// Get metadata hash
	metadataHash, err := s.getMetadataHash(ctx, contractID)
	if err != nil {
		s.logger.Warn("failed to get metadata hash", "contract_id", contractID, "error", err)
		metadataHash = ""
	}

	// Calculate prices using LMSR formula
	priceYes, priceNo := calculatePrices(yesSold, noSold)

	return &MarketState{
		ContractID:   contractID,
		YesSold:      yesSold,
		NoSold:       noSold,
		Pool:         pool,
		Resolved:     resolved,
		MetadataHash: metadataHash,
		PriceYes:     priceYes,
		PriceNo:      priceNo,
	}, nil
}

// getMetadataHash fetches metadata hash from contract.
func (s *FactoryService) getMetadataHash(ctx context.Context, contractID string) (string, error) {
	txXDR, err := s.txBuilder.BuildGetMetadataHashTx(ctx, stellar.GetMetadataHashTxParams{
		UserPublicKey: s.oraclePublicKey,
		ContractID:    contractID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to build get_metadata_hash tx: %w", err)
	}

	simResult, err := s.sorobanClient.SimulateTransaction(ctx, txXDR)
	if err != nil {
		return "", fmt.Errorf("failed to simulate get_metadata_hash: %w", err)
	}

	if simResult.Error != "" {
		return "", fmt.Errorf("simulation error: %s", simResult.Error)
	}

	if len(simResult.Results) == 0 || simResult.Results[0].XDR == "" {
		return "", fmt.Errorf("no result from simulation")
	}

	returnVal, err := soroban.ParseReturnValue(simResult.Results[0].XDR)
	if err != nil {
		return "", fmt.Errorf("failed to parse return value: %w", err)
	}

	hash, err := soroban.DecodeString(returnVal)
	if err != nil {
		return "", fmt.Errorf("failed to decode metadata hash: %w", err)
	}

	return hash, nil
}

// calculatePrices calculates YES and NO prices using LMSR formula.
// Returns prices as floats between 0 and 1.
//
// NOTE: This is a placeholder implementation. Accurate LMSR prices require
// the liquidity parameter (b) which is not returned by get_state().
// TODO: Either add liquidity_param to get_state() return value, or call
// get_price() for each market (additional RPC call per market).
func calculatePrices(yesSold, noSold int64) (priceYes, priceNo float64) {
	// At equilibrium (yesSold == noSold), both prices are 0.5
	if yesSold == 0 && noSold == 0 {
		return 0.5, 0.5
	}

	// Without b, we can only estimate relative prices based on quantity ratio.
	// This is a rough approximation for display purposes only.
	total := float64(yesSold + noSold)
	if total == 0 {
		return 0.5, 0.5
	}

	// In LMSR, higher quantity sold means higher price (more demand).
	// Simple ratio-based estimate (not true LMSR, but directionally correct)
	priceYes = float64(yesSold) / total
	priceNo = float64(noSold) / total

	// Clamp to reasonable range
	if priceYes < 0.01 {
		priceYes = 0.01
	}
	if priceNo < 0.01 {
		priceNo = 0.01
	}

	// Normalize so they sum to 1
	sum := priceYes + priceNo
	priceYes /= sum
	priceNo /= sum

	return priceYes, priceNo
}

// DeployMarketRequest contains data for deploying a new market.
type DeployMarketRequest struct {
	LiquidityParam float64
	MetadataHash   string
	InitialFunding float64
}

// Validate validates the deploy request.
func (r *DeployMarketRequest) Validate() error {
	if r.LiquidityParam <= 0 {
		return model.ErrInvalidLiquidityParam
	}
	if r.MetadataHash == "" {
		return ErrInvalidMetadataHash
	}
	// Initial funding must be at least 70% of liquidity param (b * ln(2) ≈ 0.693)
	minFunding := r.LiquidityParam * 0.7
	if r.InitialFunding < minFunding {
		return fmt.Errorf("initial funding must be at least %.2f (70%% of liquidity parameter)", minFunding)
	}
	return nil
}

// BuildDeployMarketTx builds a transaction to deploy a new market via factory.
func (s *FactoryService) BuildDeployMarketTx(ctx context.Context, req DeployMarketRequest) (*model.TransactionResult, error) {
	if s.factoryContract == "" {
		return nil, ErrFactoryNotConfigured
	}

	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("deploy request validation failed: %w", err)
	}

	// Generate random salt
	var salt [32]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	// Convert to scaled int64
	liquidityParam, err := safeFloatToInt64(req.LiquidityParam * float64(soroban.ScaleFactor))
	if err != nil {
		return nil, fmt.Errorf("invalid liquidity parameter: %w", err)
	}

	initialFunding, err := safeFloatToInt64(req.InitialFunding * float64(soroban.ScaleFactor))
	if err != nil {
		return nil, fmt.Errorf("invalid initial funding: %w", err)
	}

	txXDR, err := s.txBuilder.BuildDeployMarketTx(ctx, stellar.DeployMarketTxParams{
		OraclePublicKey: s.oraclePublicKey,
		FactoryContract: s.factoryContract,
		LiquidityParam:  liquidityParam,
		MetadataHash:    req.MetadataHash,
		InitialFunding:  initialFunding,
		Salt:            salt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build deploy transaction: %w", err)
	}

	preparedXDR, err := s.txBuilder.SimulateAndPrepareTx(ctx, txXDR)
	if err != nil {
		return nil, fmt.Errorf("failed to simulate transaction: %w", err)
	}

	return &model.TransactionResult{
		XDR:         preparedXDR,
		Description: fmt.Sprintf("Deploy new market (b=%.2f, funding=%.2f)", req.LiquidityParam, req.InitialFunding),
		SignWith:    s.oraclePublicKey,
		SubmitURL:   s.sorobanClient.RPCURL(),
	}, nil
}
