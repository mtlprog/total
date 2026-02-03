package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/mtlprog/total/internal/config"
	"github.com/mtlprog/total/internal/ipfs"
	"github.com/mtlprog/total/internal/lmsr"
	"github.com/mtlprog/total/internal/model"
	"github.com/mtlprog/total/internal/stellar"
	"github.com/stellar/go-stellar-sdk/keypair"
)

var (
	ErrMarketNotFound   = errors.New("market not found")
	ErrMarketResolved   = errors.New("market already resolved")
	ErrInvalidOutcome   = errors.New("invalid outcome")
	ErrInsufficientCost = errors.New("insufficient cost provided")
)

// MarketService handles prediction market operations.
type MarketService struct {
	stellarClient  stellar.Client
	txBuilder      *stellar.Builder
	ipfsClient     *ipfs.Client
	oraclePublicKey string
	logger         *slog.Logger
}

// NewMarketService creates a new market service.
func NewMarketService(
	stellarClient stellar.Client,
	txBuilder *stellar.Builder,
	ipfsClient *ipfs.Client,
	oraclePublicKey string,
	logger *slog.Logger,
) *MarketService {
	return &MarketService{
		stellarClient:  stellarClient,
		txBuilder:      txBuilder,
		ipfsClient:     ipfsClient,
		oraclePublicKey: oraclePublicKey,
		logger:         logger,
	}
}

// GetMarket retrieves a market by its ID (account public key).
func (s *MarketService) GetMarket(ctx context.Context, marketID string) (*model.Market, error) {
	data, err := s.stellarClient.GetAccountData(ctx, marketID)
	if err != nil {
		if errors.Is(err, stellar.ErrAccountNotFound) {
			return nil, ErrMarketNotFound
		}
		return nil, fmt.Errorf("failed to get market data: %w", err)
	}

	// Decode data entries
	ipfsHash := decodeData(data["ipfs"])
	bParam := decodeData(data["b"])
	yesCode := decodeData(data["yes"])
	noCode := decodeData(data["no"])
	resolution := decodeData(data["resolution"])

	// Parse liquidity parameter
	liquidityParam, _ := strconv.ParseFloat(bParam, 64)
	if liquidityParam <= 0 {
		liquidityParam = config.DefaultLiquidityParam
	}

	// Get balances to calculate tokens sold
	balances, err := s.stellarClient.GetAccountBalances(ctx, marketID)
	if err != nil {
		return nil, fmt.Errorf("failed to get balances: %w", err)
	}

	var yesSold, noSold float64
	for _, b := range balances {
		if b.Asset.Code == yesCode {
			balance, _ := strconv.ParseFloat(b.Balance, 64)
			yesSold = config.InitialTokenSupply - balance
		}
		if b.Asset.Code == noCode {
			balance, _ := strconv.ParseFloat(b.Balance, 64)
			noSold = config.InitialTokenSupply - balance
		}
	}

	// Calculate current prices using LMSR
	calc, _ := lmsr.New(liquidityParam)
	priceYes, priceNo, _ := calc.Price(yesSold, noSold)

	// Fetch metadata from IPFS
	var metadata model.MarketMetadata
	if ipfsHash != "" {
		if err := s.ipfsClient.GetJSON(ctx, ipfsHash, &metadata); err != nil {
			s.logger.Warn("failed to fetch market metadata", "hash", ipfsHash, "error", err)
		}
	}

	market := &model.Market{
		ID:              marketID,
		Question:        metadata.Question,
		Description:     metadata.Description,
		YesAsset:        yesCode,
		NoAsset:         noCode,
		CollateralAsset: fmt.Sprintf("%s:%s", config.EURMTLCode, config.EURMTLIssuer),
		LiquidityParam:  liquidityParam,
		YesSold:         yesSold,
		NoSold:          noSold,
		PriceYes:        priceYes,
		PriceNo:         priceNo,
		Resolution:      resolution,
		MetadataHash:    ipfsHash,
		CreatedAt:       metadata.CreatedAt,
	}

	return market, nil
}

// ListMarkets returns all markets created by the oracle.
// Note: In a real implementation, this would query Horizon for accounts
// created by the oracle. For now, we need to track market IDs externally.
func (s *MarketService) ListMarkets(ctx context.Context, marketIDs []string) ([]*model.Market, error) {
	var markets []*model.Market
	for _, id := range marketIDs {
		market, err := s.GetMarket(ctx, id)
		if err != nil {
			s.logger.Warn("failed to get market", "id", id, "error", err)
			continue
		}
		markets = append(markets, market)
	}
	return markets, nil
}

// GetQuote calculates a price quote for buying outcome tokens.
func (s *MarketService) GetQuote(ctx context.Context, marketID, outcome string, amount float64) (*model.PriceQuote, error) {
	if outcome != "YES" && outcome != "NO" {
		return nil, ErrInvalidOutcome
	}

	market, err := s.GetMarket(ctx, marketID)
	if err != nil {
		return nil, err
	}

	if market.IsResolved() {
		return nil, ErrMarketResolved
	}

	calc, _ := lmsr.New(market.LiquidityParam)
	cost, pricePerShare, newProb, err := calc.Quote(market.YesSold, market.NoSold, amount, outcome)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate quote: %w", err)
	}

	return &model.PriceQuote{
		MarketID:       marketID,
		Outcome:        outcome,
		ShareAmount:    amount,
		Cost:           cost,
		PricePerShare:  pricePerShare,
		NewProbability: newProb,
	}, nil
}

// CreateMarket creates a new prediction market.
// Returns the XDR transaction and the new market's public key.
func (s *MarketService) CreateMarket(ctx context.Context, req model.CreateMarketRequest) (*model.TransactionResult, string, error) {
	// Generate new keypair for market account
	marketKP, err := keypair.Random()
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate market keypair: %w", err)
	}

	// Create metadata for IPFS
	metadata := model.MarketMetadata{
		Question:        req.Question,
		Description:     req.Description,
		CloseTime:       req.CloseTime,
		LiquidityParam:  req.LiquidityParam,
		CollateralAsset: fmt.Sprintf("%s:%s", config.EURMTLCode, config.EURMTLIssuer),
		CreatedBy:       req.OraclePublicKey,
		CreatedAt:       time.Now().UTC(),
	}

	// Pin metadata to IPFS
	ipfsHash, err := s.ipfsClient.PinJSON(ctx, metadata)
	if err != nil {
		return nil, "", fmt.Errorf("failed to pin metadata: %w", err)
	}

	// Calculate initial funding (LMSR initial liquidity)
	calc, _ := lmsr.New(req.LiquidityParam)
	initialFunding := calc.InitialLiquidity()

	// Build transaction
	xdr, err := s.txBuilder.BuildCreateMarketTx(ctx, stellar.CreateMarketTxParams{
		OraclePublicKey: req.OraclePublicKey,
		MarketKeypair:   marketKP,
		MetadataHash:    ipfsHash,
		LiquidityParam:  req.LiquidityParam,
		InitialFunding:  initialFunding,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to build transaction: %w", err)
	}

	result := &model.TransactionResult{
		XDR:         xdr,
		Description: fmt.Sprintf("Create market: %s", req.Question),
		SignWith:    req.OraclePublicKey,
		SubmitURL:   s.stellarClient.HorizonURL() + "/transactions",
	}

	return result, marketKP.Address(), nil
}

// BuildBuyTx builds a transaction for buying outcome tokens.
func (s *MarketService) BuildBuyTx(ctx context.Context, req model.BuyRequest) (*model.TransactionResult, error) {
	// Get quote to determine cost
	quote, err := s.GetQuote(ctx, req.MarketID, req.Outcome, req.ShareAmount)
	if err != nil {
		return nil, err
	}

	// Build transaction
	xdr, err := s.txBuilder.BuildBuyTokenTx(ctx, stellar.BuyTokenTxParams{
		UserPublicKey:   req.UserPublicKey,
		MarketPublicKey: req.MarketID,
		Outcome:         req.Outcome,
		TokenAmount:     req.ShareAmount,
		MaxCost:         quote.Cost * 1.01, // 1% slippage buffer
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build transaction: %w", err)
	}

	return &model.TransactionResult{
		XDR:         xdr,
		Description: fmt.Sprintf("Buy %.2f %s tokens for %.4f EURMTL", req.ShareAmount, req.Outcome, quote.Cost),
		SignWith:    req.UserPublicKey,
		SubmitURL:   s.stellarClient.HorizonURL() + "/transactions",
	}, nil
}

// BuildResolveTx builds a transaction to resolve a market.
func (s *MarketService) BuildResolveTx(ctx context.Context, req model.ResolveRequest) (*model.TransactionResult, error) {
	if req.WinningOutcome != "YES" && req.WinningOutcome != "NO" {
		return nil, ErrInvalidOutcome
	}

	market, err := s.GetMarket(ctx, req.MarketID)
	if err != nil {
		return nil, err
	}

	if market.IsResolved() {
		return nil, ErrMarketResolved
	}

	xdr, err := s.txBuilder.BuildResolveTx(ctx, stellar.ResolveTxParams{
		OraclePublicKey: req.OraclePublicKey,
		MarketPublicKey: req.MarketID,
		WinningOutcome:  req.WinningOutcome,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build transaction: %w", err)
	}

	return &model.TransactionResult{
		XDR:         xdr,
		Description: fmt.Sprintf("Resolve market: %s wins", req.WinningOutcome),
		SignWith:    req.OraclePublicKey,
		SubmitURL:   s.stellarClient.HorizonURL() + "/transactions",
	}, nil
}

// GetPriceHistory returns historical prices for a market.
func (s *MarketService) GetPriceHistory(ctx context.Context, marketID string, limit int) ([]model.PricePoint, error) {
	// Get market for liquidity parameter
	market, err := s.GetMarket(ctx, marketID)
	if err != nil {
		return nil, err
	}

	// Get operations to reconstruct price history
	ops, err := s.stellarClient.GetOperations(ctx, marketID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get operations: %w", err)
	}

	// For now, return current price as single point
	// In a full implementation, we'd parse payment operations to reconstruct history
	points := []model.PricePoint{
		{
			Timestamp: time.Now(),
			PriceYes:  market.PriceYes,
		},
	}

	// Reverse iterate through operations to build history
	// This is simplified - real implementation would track cumulative trades
	_ = ops // TODO: implement full history reconstruction

	return points, nil
}

// decodeData decodes base64 data entry.
func decodeData(encoded string) string {
	if encoded == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	return string(decoded)
}
