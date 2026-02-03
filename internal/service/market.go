package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mtlprog/total/internal/model"
	"github.com/mtlprog/total/internal/soroban"
	"github.com/mtlprog/total/internal/stellar"
)

var (
	ErrMarketNotFound   = errors.New("market not found")
	ErrMarketResolved   = errors.New("market already resolved")
	ErrInvalidOutcome   = errors.New("invalid outcome")
	ErrInsufficientCost = errors.New("insufficient cost provided")
)

// MarketService handles prediction market operations via Soroban contracts.
type MarketService struct {
	stellarClient   stellar.Client
	sorobanClient   *soroban.Client
	txBuilder       *stellar.Builder
	oraclePublicKey string
	logger          *slog.Logger
}

// NewMarketService creates a new market service.
func NewMarketService(
	stellarClient stellar.Client,
	sorobanClient *soroban.Client,
	txBuilder *stellar.Builder,
	oraclePublicKey string,
	logger *slog.Logger,
) *MarketService {
	return &MarketService{
		stellarClient:   stellarClient,
		sorobanClient:   sorobanClient,
		txBuilder:       txBuilder,
		oraclePublicKey: oraclePublicKey,
		logger:          logger,
	}
}

// TradeRequest contains common fields for buy/sell operations.
type TradeRequest struct {
	UserPublicKey string
	ContractID    string
	Outcome       model.Outcome
	ShareAmount   float64
	Slippage      float64
}

// Validate validates the trade request fields.
func (r *TradeRequest) Validate() error {
	if err := model.ValidateStellarPublicKey(r.UserPublicKey); err != nil {
		return err
	}
	if err := soroban.ValidateContractID(r.ContractID); err != nil {
		return err
	}
	if !r.Outcome.IsValid() {
		return model.ErrInvalidOutcome
	}
	if r.ShareAmount <= 0 {
		return model.ErrInvalidShareAmount
	}
	if r.Slippage <= 0 || r.Slippage > model.MaxSlippage {
		return model.ErrInvalidSlippage
	}
	return nil
}

// BuyRequest contains data for buying outcome tokens.
type BuyRequest struct {
	TradeRequest
}

// SellRequest contains data for selling tokens.
type SellRequest struct {
	TradeRequest
}

// BuildBuyTx builds a transaction for buying tokens.
func (s *MarketService) BuildBuyTx(ctx context.Context, req BuyRequest) (*model.TransactionResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get quote to calculate max cost
	quote, err := s.GetQuote(ctx, req.ContractID, req.Outcome, req.ShareAmount)
	if err != nil {
		return nil, fmt.Errorf("failed to get quote: %w", err)
	}

	// Validate quote cost
	if quote.Cost <= 0 {
		return nil, fmt.Errorf("invalid quote cost: %d (expected positive value)", quote.Cost)
	}

	// Apply slippage
	maxCost := int64(float64(quote.Cost) * (1 + req.Slippage))

	amount := int64(req.ShareAmount * float64(soroban.ScaleFactor))

	outcomeU32, err := soroban.OutcomeToU32(string(req.Outcome))
	if err != nil {
		return nil, fmt.Errorf("invalid outcome: %w", err)
	}

	txXDR, err := s.txBuilder.BuildBuyTx(ctx, stellar.BuyTxParams{
		UserPublicKey: req.UserPublicKey,
		ContractID:    req.ContractID,
		Outcome:       outcomeU32,
		Amount:        amount,
		MaxCost:       maxCost,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build transaction: %w", err)
	}

	preparedXDR, err := s.txBuilder.SimulateAndPrepareTx(ctx, txXDR)
	if err != nil {
		return nil, fmt.Errorf("failed to simulate transaction: %w", err)
	}

	return &model.TransactionResult{
		XDR:         preparedXDR,
		Description: fmt.Sprintf("Buy %.2f %s tokens", req.ShareAmount, req.Outcome),
		SignWith:    req.UserPublicKey,
		SubmitURL:   s.sorobanClient.RPCURL(),
	}, nil
}

// BuildSellTx builds a transaction for selling tokens.
func (s *MarketService) BuildSellTx(ctx context.Context, req SellRequest) (*model.TransactionResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get sell quote for accurate return calculation
	sellQuote, err := s.GetSellQuote(ctx, req.ContractID, req.Outcome, req.ShareAmount)
	if err != nil {
		return nil, fmt.Errorf("failed to get sell quote: %w", err)
	}

	// Validate return amount
	if sellQuote.ReturnAmount <= 0 {
		return nil, fmt.Errorf("invalid sell return: %d (expected positive value)", sellQuote.ReturnAmount)
	}

	// Apply slippage protection
	minReturn := int64(float64(sellQuote.ReturnAmount) * (1 - req.Slippage))

	amount := int64(req.ShareAmount * float64(soroban.ScaleFactor))

	outcomeU32, err := soroban.OutcomeToU32(string(req.Outcome))
	if err != nil {
		return nil, fmt.Errorf("invalid outcome: %w", err)
	}

	txXDR, err := s.txBuilder.BuildSellTx(ctx, stellar.SellTxParams{
		UserPublicKey: req.UserPublicKey,
		ContractID:    req.ContractID,
		Outcome:       outcomeU32,
		Amount:        amount,
		MinReturn:     minReturn,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build transaction: %w", err)
	}

	preparedXDR, err := s.txBuilder.SimulateAndPrepareTx(ctx, txXDR)
	if err != nil {
		return nil, fmt.Errorf("failed to simulate transaction: %w", err)
	}

	return &model.TransactionResult{
		XDR:         preparedXDR,
		Description: fmt.Sprintf("Sell %.2f %s tokens", req.ShareAmount, req.Outcome),
		SignWith:    req.UserPublicKey,
		SubmitURL:   s.sorobanClient.RPCURL(),
	}, nil
}

// ResolveRequest contains data for resolving a market.
type ResolveRequest struct {
	OraclePublicKey string
	ContractID      string
	WinningOutcome  model.Outcome
}

// Validate validates the resolve request.
func (r *ResolveRequest) Validate() error {
	if err := model.ValidateStellarPublicKey(r.OraclePublicKey); err != nil {
		return err
	}
	if err := soroban.ValidateContractID(r.ContractID); err != nil {
		return err
	}
	if !r.WinningOutcome.IsValid() {
		return model.ErrInvalidOutcome
	}
	return nil
}

// BuildResolveTx builds a transaction to resolve a market.
func (s *MarketService) BuildResolveTx(ctx context.Context, req ResolveRequest) (*model.TransactionResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	outcomeU32, err := soroban.OutcomeToU32(string(req.WinningOutcome))
	if err != nil {
		return nil, fmt.Errorf("invalid outcome: %w", err)
	}

	txXDR, err := s.txBuilder.BuildResolveTx(ctx, stellar.ResolveTxParams{
		OraclePublicKey: req.OraclePublicKey,
		ContractID:      req.ContractID,
		WinningOutcome:  outcomeU32,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build transaction: %w", err)
	}

	preparedXDR, err := s.txBuilder.SimulateAndPrepareTx(ctx, txXDR)
	if err != nil {
		return nil, fmt.Errorf("failed to simulate transaction: %w", err)
	}

	return &model.TransactionResult{
		XDR:         preparedXDR,
		Description: fmt.Sprintf("Resolve market: %s wins", req.WinningOutcome),
		SignWith:    req.OraclePublicKey,
		SubmitURL:   s.sorobanClient.RPCURL(),
	}, nil
}

// ClaimRequest contains data for claiming winnings.
type ClaimRequest struct {
	UserPublicKey string
	ContractID    string
}

// Validate validates the claim request.
func (r *ClaimRequest) Validate() error {
	if err := model.ValidateStellarPublicKey(r.UserPublicKey); err != nil {
		return err
	}
	if err := soroban.ValidateContractID(r.ContractID); err != nil {
		return err
	}
	return nil
}

// BuildClaimTx builds a transaction to claim winnings.
func (s *MarketService) BuildClaimTx(ctx context.Context, req ClaimRequest) (*model.TransactionResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	txXDR, err := s.txBuilder.BuildClaimTx(ctx, stellar.ClaimTxParams{
		UserPublicKey: req.UserPublicKey,
		ContractID:    req.ContractID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build transaction: %w", err)
	}

	preparedXDR, err := s.txBuilder.SimulateAndPrepareTx(ctx, txXDR)
	if err != nil {
		return nil, fmt.Errorf("failed to simulate transaction: %w", err)
	}

	return &model.TransactionResult{
		XDR:         preparedXDR,
		Description: "Claim winnings",
		SignWith:    req.UserPublicKey,
		SubmitURL:   s.sorobanClient.RPCURL(),
	}, nil
}

// Quote represents a price quote for buying from the contract.
type Quote struct {
	Cost       int64   // Scaled by 10^7
	PriceAfter float64 // 0-1
}

// SellQuote represents a price quote for selling from the contract.
type SellQuote struct {
	ReturnAmount int64   // Scaled by 10^7
	PriceAfter   float64 // 0-1
}

// GetQuote gets a price quote from a market contract.
func (s *MarketService) GetQuote(ctx context.Context, contractID string, outcome model.Outcome, amount float64) (*Quote, error) {
	amountScaled := int64(amount * float64(soroban.ScaleFactor))

	outcomeU32, err := soroban.OutcomeToU32(string(outcome))
	if err != nil {
		return nil, fmt.Errorf("invalid outcome: %w", err)
	}

	txXDR, err := s.txBuilder.BuildGetQuoteTx(ctx, stellar.GetQuoteTxParams{
		UserPublicKey: s.oraclePublicKey,
		ContractID:    contractID,
		Outcome:       outcomeU32,
		Amount:        amountScaled,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build quote transaction: %w", err)
	}

	simResult, err := s.sorobanClient.SimulateTransaction(ctx, txXDR)
	if err != nil {
		return nil, fmt.Errorf("failed to simulate quote: %w", err)
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

	// Contract returns tuple (cost: i128, price_after: i128)
	tuple, err := soroban.DecodeVec(returnVal)
	if err != nil {
		return nil, fmt.Errorf("failed to decode quote result: expected (cost, price_after) tuple, got %v: %w", returnVal.Type, err)
	}

	if len(tuple) < 2 {
		return nil, fmt.Errorf("expected tuple of 2 elements, got %d", len(tuple))
	}

	cost, err := soroban.DecodeI128(tuple[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode cost from tuple: %w", err)
	}

	priceAfterScaled, err := soroban.DecodeI128(tuple[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode price_after from tuple: %w", err)
	}

	// Convert price_after from scaled i128 to float64 (0-1)
	priceAfter := float64(priceAfterScaled) / float64(soroban.ScaleFactor)

	return &Quote{
		Cost:       cost,
		PriceAfter: priceAfter,
	}, nil
}

// GetSellQuote gets a sell price quote from a market contract.
func (s *MarketService) GetSellQuote(ctx context.Context, contractID string, outcome model.Outcome, amount float64) (*SellQuote, error) {
	amountScaled := int64(amount * float64(soroban.ScaleFactor))

	outcomeU32, err := soroban.OutcomeToU32(string(outcome))
	if err != nil {
		return nil, fmt.Errorf("invalid outcome: %w", err)
	}

	txXDR, err := s.txBuilder.BuildGetSellQuoteTx(ctx, stellar.GetQuoteTxParams{
		UserPublicKey: s.oraclePublicKey,
		ContractID:    contractID,
		Outcome:       outcomeU32,
		Amount:        amountScaled,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build sell quote transaction: %w", err)
	}

	simResult, err := s.sorobanClient.SimulateTransaction(ctx, txXDR)
	if err != nil {
		return nil, fmt.Errorf("failed to simulate sell quote: %w", err)
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

	// Contract returns tuple (return_amount: i128, price_after: i128)
	tuple, err := soroban.DecodeVec(returnVal)
	if err != nil {
		return nil, fmt.Errorf("failed to decode sell quote result: expected (return_amount, price_after) tuple, got %v: %w", returnVal.Type, err)
	}

	if len(tuple) < 2 {
		return nil, fmt.Errorf("expected tuple of 2 elements, got %d", len(tuple))
	}

	returnAmount, err := soroban.DecodeI128(tuple[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode return_amount from tuple: %w", err)
	}

	priceAfterScaled, err := soroban.DecodeI128(tuple[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode price_after from tuple: %w", err)
	}

	// Convert price_after from scaled i128 to float64 (0-1)
	priceAfter := float64(priceAfterScaled) / float64(soroban.ScaleFactor)

	return &SellQuote{
		ReturnAmount: returnAmount,
		PriceAfter:   priceAfter,
	}, nil
}
