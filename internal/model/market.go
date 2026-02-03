package model

import "time"

// Market represents a prediction market on Stellar.
type Market struct {
	ID              string     `json:"id"`                // Market account public key
	Question        string     `json:"question"`          // Main question
	Description     string     `json:"description"`       // Detailed description
	YesAsset        string     `json:"yes_asset"`         // YES token asset code
	NoAsset         string     `json:"no_asset"`          // NO token asset code
	CollateralAsset string     `json:"collateral_asset"`  // e.g., "EURMTL:ISSUER"
	LiquidityParam  float64    `json:"liquidity_param"`   // LMSR b parameter
	YesSold         float64    `json:"yes_sold"`          // Tokens sold
	NoSold          float64    `json:"no_sold"`           // Tokens sold
	PriceYes        float64    `json:"price_yes"`         // Current YES price (0-1)
	PriceNo         float64    `json:"price_no"`          // Current NO price (0-1)
	ResolvedAt      *time.Time `json:"resolved_at"`       // Resolution timestamp
	Resolution      string     `json:"resolution"`        // "YES", "NO", or ""
	CreatedAt       time.Time  `json:"created_at"`        // Creation timestamp
	MetadataHash    string     `json:"metadata_hash"`     // IPFS hash
}

// IsResolved returns true if the market has been resolved.
func (m *Market) IsResolved() bool {
	return m.Resolution != ""
}

// MarketMetadata is stored in IPFS.
type MarketMetadata struct {
	Question        string    `json:"question"`
	Description     string    `json:"description"`
	CloseTime       time.Time `json:"close_time"`
	LiquidityParam  float64   `json:"liquidity_param"`
	CollateralAsset string    `json:"collateral_asset"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
}

// PriceQuote represents a quote for buying outcome tokens.
type PriceQuote struct {
	MarketID       string  `json:"market_id"`
	Outcome        string  `json:"outcome"`         // "YES" or "NO"
	ShareAmount    float64 `json:"share_amount"`    // Tokens to buy
	Cost           float64 `json:"cost"`            // Cost in collateral
	PricePerShare  float64 `json:"price_per_share"` // Average price
	NewProbability float64 `json:"new_probability"` // Probability after purchase
}

// PricePoint represents a historical price for charting.
type PricePoint struct {
	Timestamp time.Time `json:"timestamp"`
	PriceYes  float64   `json:"price_yes"`
}

// CreateMarketRequest contains data for creating a new market.
type CreateMarketRequest struct {
	Question        string    `json:"question"`
	Description     string    `json:"description"`
	CloseTime       time.Time `json:"close_time"`
	LiquidityParam  float64   `json:"liquidity_param"`
	OraclePublicKey string    `json:"oracle_public_key"`
}

// BuyRequest contains data for buying outcome tokens.
type BuyRequest struct {
	UserPublicKey string  `json:"user_public_key"`
	MarketID      string  `json:"market_id"`
	Outcome       string  `json:"outcome"`      // "YES" or "NO"
	ShareAmount   float64 `json:"share_amount"` // Amount to buy
}

// ResolveRequest contains data for resolving a market.
type ResolveRequest struct {
	MarketID        string `json:"market_id"`
	WinningOutcome  string `json:"winning_outcome"` // "YES" or "NO"
	OraclePublicKey string `json:"oracle_public_key"`
}

// TransactionResult is returned after building a transaction.
type TransactionResult struct {
	XDR         string `json:"xdr"`          // Base64 encoded XDR
	Description string `json:"description"`  // Human-readable description
	SignWith    string `json:"sign_with"`    // Public key that must sign
	SubmitURL   string `json:"submit_url"`   // Horizon submit URL
}
