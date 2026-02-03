package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math"
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
	ErrIPFSNotConfigured = errors.New("IPFS client not configured")
	ErrInvalidMarketData = errors.New("invalid market data")
)

// MarketService handles prediction market operations.
type MarketService struct {
	stellarClient   stellar.Client
	txBuilder       *stellar.Builder
	ipfsClient      *ipfs.Client
	oraclePublicKey string
	logger          *slog.Logger
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
		stellarClient:   stellarClient,
		txBuilder:       txBuilder,
		ipfsClient:      ipfsClient,
		oraclePublicKey: oraclePublicKey,
		logger:          logger,
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
	ipfsHash, err := decodeData(data["ipfs"])
	if err != nil {
		s.logger.Warn("failed to decode ipfs hash", "marketID", marketID, "error", err)
	}

	bParam, err := decodeData(data["b"])
	if err != nil {
		s.logger.Warn("failed to decode liquidity param", "marketID", marketID, "error", err)
	}

	yesCode, err := decodeData(data["yes"])
	if err != nil {
		s.logger.Warn("failed to decode yes asset code", "marketID", marketID, "error", err)
	}

	noCode, err := decodeData(data["no"])
	if err != nil {
		s.logger.Warn("failed to decode no asset code", "marketID", marketID, "error", err)
	}

	resolutionStr, err := decodeData(data["resolution"])
	if err != nil {
		s.logger.Warn("failed to decode resolution", "marketID", marketID, "error", err)
	}

	// Parse resolution to Outcome type
	var resolution model.Outcome
	if resolutionStr != "" {
		resolution, err = model.ParseOutcome(resolutionStr)
		if err != nil {
			s.logger.Warn("invalid resolution value", "marketID", marketID, "resolution", resolutionStr)
		}
	}

	// Parse liquidity parameter
	liquidityParam, err := strconv.ParseFloat(bParam, 64)
	if err != nil || liquidityParam <= 0 {
		s.logger.Warn("invalid liquidity param, using default", "marketID", marketID, "bParam", bParam)
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
			balance, parseErr := strconv.ParseFloat(b.Balance, 64)
			if parseErr != nil {
				s.logger.Warn("failed to parse YES balance", "balance", b.Balance, "error", parseErr)
				continue
			}
			yesSold = math.Max(0, config.InitialTokenSupply-balance)
		}
		if b.Asset.Code == noCode {
			balance, parseErr := strconv.ParseFloat(b.Balance, 64)
			if parseErr != nil {
				s.logger.Warn("failed to parse NO balance", "balance", b.Balance, "error", parseErr)
				continue
			}
			noSold = math.Max(0, config.InitialTokenSupply-balance)
		}
	}

	// Calculate current prices using LMSR
	calc, err := lmsr.New(liquidityParam)
	if err != nil {
		return nil, fmt.Errorf("invalid liquidity parameter: %w", err)
	}

	priceYes, priceNo, err := calc.Price(yesSold, noSold)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate prices: %w", err)
	}

	// Fetch metadata from IPFS
	var metadata model.MarketMetadata
	if ipfsHash != "" && s.ipfsClient != nil {
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

// ListMarketsResult contains the result of listing markets.
type ListMarketsResult struct {
	Markets      []*model.Market
	FailedCount  int
	FailedIDs    []string
}

// ListMarkets returns all markets created by the oracle.
// Returns partial results if some markets fail to load.
func (s *MarketService) ListMarkets(ctx context.Context, marketIDs []string) ([]*model.Market, error) {
	var markets []*model.Market
	var failedIDs []string

	for _, id := range marketIDs {
		market, err := s.GetMarket(ctx, id)
		if err != nil {
			s.logger.Warn("failed to get market", "id", id, "error", err)
			failedIDs = append(failedIDs, id)
			continue
		}
		markets = append(markets, market)
	}

	// Log if all markets failed
	if len(marketIDs) > 0 && len(markets) == 0 {
		s.logger.Error("all markets failed to load", "total", len(marketIDs), "failed", failedIDs)
	} else if len(failedIDs) > 0 {
		s.logger.Warn("some markets failed to load", "total", len(marketIDs), "failed", len(failedIDs))
	}

	return markets, nil
}

// GetQuote calculates a price quote for buying outcome tokens.
func (s *MarketService) GetQuote(ctx context.Context, marketID string, outcome model.Outcome, amount float64) (*model.PriceQuote, error) {
	if !outcome.IsValid() {
		return nil, ErrInvalidOutcome
	}

	market, err := s.GetMarket(ctx, marketID)
	if err != nil {
		return nil, err
	}

	if market.IsResolved() {
		return nil, ErrMarketResolved
	}

	calc, err := lmsr.New(market.LiquidityParam)
	if err != nil {
		return nil, fmt.Errorf("invalid liquidity parameter: %w", err)
	}

	cost, pricePerShare, newProb, err := calc.Quote(market.YesSold, market.NoSold, amount, outcome.String())
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
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, "", err
	}

	// Check IPFS client is configured
	if s.ipfsClient == nil {
		return nil, "", ErrIPFSNotConfigured
	}

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
	calc, err := lmsr.New(req.LiquidityParam)
	if err != nil {
		return nil, "", fmt.Errorf("invalid liquidity parameter: %w", err)
	}
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
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get quote to determine cost
	quote, err := s.GetQuote(ctx, req.MarketID, req.Outcome, req.ShareAmount)
	if err != nil {
		return nil, err
	}

	// Apply slippage buffer
	maxCost := quote.Cost * (1 + req.Slippage)

	// Build transaction
	xdr, err := s.txBuilder.BuildBuyTokenTx(ctx, stellar.BuyTokenTxParams{
		UserPublicKey:   req.UserPublicKey,
		MarketPublicKey: req.MarketID,
		Outcome:         req.Outcome.String(),
		TokenAmount:     req.ShareAmount,
		MaxCost:         maxCost,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build transaction: %w", err)
	}

	return &model.TransactionResult{
		XDR:         xdr,
		Description: fmt.Sprintf("Buy %.2f %s tokens for %.4f EURMTL (max)", req.ShareAmount, req.Outcome, maxCost),
		SignWith:    req.UserPublicKey,
		SubmitURL:   s.stellarClient.HorizonURL() + "/transactions",
	}, nil
}

// BuildResolveTx builds a transaction to resolve a market.
func (s *MarketService) BuildResolveTx(ctx context.Context, req model.ResolveRequest) (*model.TransactionResult, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
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
		WinningOutcome:  req.WinningOutcome.String(),
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
// Returns an error if decoding fails.
func decodeData(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}
	return string(decoded), nil
}
