package model

import (
	"errors"
	"strings"
	"time"
)

// Validation errors.
var (
	ErrInvalidOutcome        = errors.New("invalid outcome: must be YES or NO")
	ErrInvalidPublicKey      = errors.New("invalid Stellar public key format")
	ErrEmptyQuestion         = errors.New("question is required")
	ErrQuestionTooLong       = errors.New("question exceeds maximum length (500 characters)")
	ErrDescriptionTooLong    = errors.New("description exceeds maximum length (2000 characters)")
	ErrInvalidLiquidityParam = errors.New("liquidity parameter must be positive")
	ErrInvalidShareAmount    = errors.New("share amount must be positive")
	ErrCloseTimeInPast       = errors.New("close time must be in the future")
	ErrInvalidSlippage       = errors.New("slippage must be between 0 and 10%")
)

const (
	MaxQuestionLength    = 500
	MaxDescriptionLength = 2000
	DefaultSlippage      = 0.01 // 1%
	MaxSlippage          = 0.10 // 10%
)

// Outcome represents a market outcome (YES or NO).
type Outcome string

const (
	OutcomeYes Outcome = "YES"
	OutcomeNo  Outcome = "NO"
)

// ParseOutcome parses a string into an Outcome.
func ParseOutcome(s string) (Outcome, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "YES":
		return OutcomeYes, nil
	case "NO":
		return OutcomeNo, nil
	default:
		return "", ErrInvalidOutcome
	}
}

// IsValid returns true if the outcome is valid.
func (o Outcome) IsValid() bool {
	return o == OutcomeYes || o == OutcomeNo
}

// String returns the string representation.
func (o Outcome) String() string {
	return string(o)
}

// ValidateStellarPublicKey validates a Stellar public key format.
// For full validation, use keypair.ParseAddress from the Stellar SDK.
func ValidateStellarPublicKey(key string) error {
	key = strings.TrimSpace(key)
	if len(key) != 56 {
		return ErrInvalidPublicKey
	}
	if !strings.HasPrefix(key, "G") {
		return ErrInvalidPublicKey
	}
	return nil
}

// Market represents a prediction market on Stellar.
type Market struct {
	ID              string     `json:"id"`               // Market account public key
	Question        string     `json:"question"`         // Main question
	Description     string     `json:"description"`      // Detailed description
	YesAsset        string     `json:"yes_asset"`        // YES token asset code
	NoAsset         string     `json:"no_asset"`         // NO token asset code
	CollateralAsset string     `json:"collateral_asset"` // e.g., "EURMTL:ISSUER"
	LiquidityParam  float64    `json:"liquidity_param"`  // LMSR b parameter
	YesSold         float64    `json:"yes_sold"`         // Tokens sold
	NoSold          float64    `json:"no_sold"`          // Tokens sold
	PriceYes        float64    `json:"price_yes"`        // Current YES price (0-1)
	PriceNo         float64    `json:"price_no"`         // Current NO price (0-1)
	ResolvedAt      *time.Time `json:"resolved_at"`      // Resolution timestamp
	Resolution      Outcome    `json:"resolution"`       // OutcomeYes, OutcomeNo, or ""
	CreatedAt       time.Time  `json:"created_at"`       // Creation timestamp
	MetadataHash    string     `json:"metadata_hash"`    // IPFS hash
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
	Outcome        Outcome `json:"outcome"`         // OutcomeYes or OutcomeNo
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

// Validate validates the create market request.
func (r *CreateMarketRequest) Validate() error {
	if strings.TrimSpace(r.Question) == "" {
		return ErrEmptyQuestion
	}
	if len(r.Question) > MaxQuestionLength {
		return ErrQuestionTooLong
	}
	if len(r.Description) > MaxDescriptionLength {
		return ErrDescriptionTooLong
	}
	if r.LiquidityParam <= 0 {
		return ErrInvalidLiquidityParam
	}
	if r.CloseTime.Before(time.Now()) {
		return ErrCloseTimeInPast
	}
	if err := ValidateStellarPublicKey(r.OraclePublicKey); err != nil {
		return err
	}
	return nil
}

// BuyRequest contains data for buying outcome tokens.
type BuyRequest struct {
	UserPublicKey string  `json:"user_public_key"`
	MarketID      string  `json:"market_id"`
	Outcome       Outcome `json:"outcome"`      // OutcomeYes or OutcomeNo
	ShareAmount   float64 `json:"share_amount"` // Amount to buy
	Slippage      float64 `json:"slippage"`     // Slippage tolerance (0.01 = 1%)
}

// Validate validates the buy request.
// Note: Does not mutate the request. Slippage defaults should be set by the caller.
func (r *BuyRequest) Validate() error {
	if err := ValidateStellarPublicKey(r.UserPublicKey); err != nil {
		return err
	}
	if err := ValidateStellarPublicKey(r.MarketID); err != nil {
		return err
	}
	if !r.Outcome.IsValid() {
		return ErrInvalidOutcome
	}
	if r.ShareAmount <= 0 {
		return ErrInvalidShareAmount
	}
	// Slippage validation - must be set by caller (0 is invalid)
	if r.Slippage <= 0 || r.Slippage > MaxSlippage {
		return ErrInvalidSlippage
	}
	return nil
}

// ResolveRequest contains data for resolving a market.
type ResolveRequest struct {
	MarketID        string  `json:"market_id"`
	WinningOutcome  Outcome `json:"winning_outcome"` // OutcomeYes or OutcomeNo
	OraclePublicKey string  `json:"oracle_public_key"`
}

// Validate validates the resolve request.
func (r *ResolveRequest) Validate() error {
	if err := ValidateStellarPublicKey(r.MarketID); err != nil {
		return err
	}
	if !r.WinningOutcome.IsValid() {
		return ErrInvalidOutcome
	}
	if err := ValidateStellarPublicKey(r.OraclePublicKey); err != nil {
		return err
	}
	return nil
}

// TransactionResult is returned after building a transaction.
type TransactionResult struct {
	XDR         string `json:"xdr"`         // Base64 encoded XDR
	Description string `json:"description"` // Human-readable description
	SignWith    string `json:"sign_with"`   // Public key that must sign
	SubmitURL   string `json:"submit_url"`  // Horizon submit URL
}
