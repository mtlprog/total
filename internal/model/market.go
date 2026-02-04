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

// NewMarket creates a new Market with required fields validated.
// ID must be a valid Soroban contract ID, liquidityParam must be positive.
func NewMarket(id string, liquidityParam float64) (*Market, error) {
	if id == "" {
		return nil, errors.New("market ID is required")
	}
	if liquidityParam <= 0 {
		return nil, ErrInvalidLiquidityParam
	}
	return &Market{
		ID:             id,
		LiquidityParam: liquidityParam,
		PriceYes:       0.5,
		PriceNo:        0.5,
	}, nil
}

// Market represents a prediction market on Stellar.
type Market struct {
	ID               string     `json:"id"`                // Market contract ID (Soroban)
	Question         string     `json:"question"`          // Main question
	Description      string     `json:"description"`       // Detailed description
	ResolutionSource string     `json:"resolution_source"` // Source for resolution (from IPFS)
	Category         string     `json:"category"`          // Market category (from IPFS)
	EndDate          time.Time  `json:"end_date"`          // Market end date (from IPFS)
	CollateralAsset  string     `json:"collateral_asset"`  // e.g., "EURMTL:ISSUER"
	LiquidityParam   float64    `json:"liquidity_param"`   // LMSR b parameter
	YesSold          float64    `json:"yes_sold"`          // Tokens sold
	NoSold           float64    `json:"no_sold"`           // Tokens sold
	PriceYes         float64    `json:"price_yes"`         // Current YES price (0-1)
	PriceNo          float64    `json:"price_no"`          // Current NO price (0-1)
	ResolvedAt       *time.Time `json:"resolved_at"`       // Resolution timestamp
	Resolution       Outcome    `json:"resolution"`        // OutcomeYes, OutcomeNo, or ""
	CreatedAt        time.Time  `json:"created_at"`        // Creation timestamp
	MetadataHash     string     `json:"metadata_hash"`     // IPFS hash
}

// IsResolved returns true if the market has been resolved.
func (m *Market) IsResolved() bool {
	return m.Resolution != ""
}

// Validate checks all market invariants.
// Returns an error if any invariant is violated.
func (m *Market) Validate() error {
	if m.ID == "" {
		return errors.New("market ID is required")
	}
	if m.LiquidityParam <= 0 {
		return ErrInvalidLiquidityParam
	}
	if m.YesSold < 0 {
		return errors.New("yes_sold must be non-negative")
	}
	if m.NoSold < 0 {
		return errors.New("no_sold must be non-negative")
	}
	if m.PriceYes < 0 || m.PriceYes > 1 {
		return errors.New("price_yes must be in range [0, 1]")
	}
	if m.PriceNo < 0 || m.PriceNo > 1 {
		return errors.New("price_no must be in range [0, 1]")
	}
	// Resolution and ResolvedAt should be consistent
	if m.Resolution != "" && !m.Resolution.IsValid() {
		return ErrInvalidOutcome
	}
	if m.Resolution != "" && m.ResolvedAt == nil {
		return errors.New("resolved market must have resolved_at timestamp")
	}
	if m.Resolution == "" && m.ResolvedAt != nil {
		return errors.New("unresolved market must not have resolved_at timestamp")
	}
	return nil
}

// MarketMetadata is defined in metadata.go

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

// TransactionResult is returned after building a transaction.
type TransactionResult struct {
	XDR         string `json:"xdr"`         // Base64 encoded XDR
	Description string `json:"description"` // Human-readable description
	SignWith    string `json:"sign_with"`   // Public key that must sign
	SubmitURL   string `json:"submit_url"`  // Horizon submit URL
}
