package model

import (
	"strings"
	"testing"
	"time"
)

func TestParseOutcome(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Outcome
		wantErr error
	}{
		{"YES uppercase", "YES", OutcomeYes, nil},
		{"NO uppercase", "NO", OutcomeNo, nil},
		{"yes lowercase", "yes", OutcomeYes, nil},
		{"no lowercase", "no", OutcomeNo, nil},
		{"Yes mixed case", "Yes", OutcomeYes, nil},
		{"No mixed case", "No", OutcomeNo, nil},
		{"YES with leading space", " YES", OutcomeYes, nil},
		{"YES with trailing space", "YES ", OutcomeYes, nil},
		{"YES with both spaces", " YES ", OutcomeYes, nil},
		{"NO with spaces", "  no  ", OutcomeNo, nil},
		{"empty string", "", "", ErrInvalidOutcome},
		{"whitespace only", "   ", "", ErrInvalidOutcome},
		{"invalid MAYBE", "MAYBE", "", ErrInvalidOutcome},
		{"partial Y", "Y", "", ErrInvalidOutcome},
		{"partial N", "N", "", ErrInvalidOutcome},
		{"invalid word", "TRUE", "", ErrInvalidOutcome},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOutcome(tt.input)
			if err != tt.wantErr {
				t.Errorf("ParseOutcome(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseOutcome(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestOutcome_IsValid(t *testing.T) {
	tests := []struct {
		outcome Outcome
		want    bool
	}{
		{OutcomeYes, true},
		{OutcomeNo, true},
		{"YES", true},
		{"NO", true},
		{"", false},
		{"MAYBE", false},
		{"yes", false}, // case-sensitive check
	}

	for _, tt := range tests {
		t.Run(string(tt.outcome), func(t *testing.T) {
			if got := tt.outcome.IsValid(); got != tt.want {
				t.Errorf("Outcome(%q).IsValid() = %v, want %v", tt.outcome, got, tt.want)
			}
		})
	}
}

func TestValidateStellarPublicKey(t *testing.T) {
	validKey := "GCEZWKCA5VLDNRLN3RPRJMRZOX3Z6G5CHCGSNFHEYVXM3XOJMDS674JZ"

	tests := []struct {
		name    string
		key     string
		wantErr error
	}{
		{"valid key", validKey, nil},
		{"valid key with spaces", " " + validKey + " ", nil},
		{"short key", "GCEZWKCA", ErrInvalidPublicKey},
		{"empty key", "", ErrInvalidPublicKey},
		{"secret key prefix", "SCEZWKCA5VLDNRLN3RPRJMRZOX3Z6G5CHCGSNFHEYVXM3XOJMDS674JZ", ErrInvalidPublicKey},
		{"wrong length", strings.Repeat("G", 55), ErrInvalidPublicKey},
		{"too long", strings.Repeat("G", 57), ErrInvalidPublicKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStellarPublicKey(tt.key)
			if err != tt.wantErr {
				t.Errorf("ValidateStellarPublicKey(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
			}
		})
	}
}

func TestCreateMarketRequest_Validate(t *testing.T) {
	validKey := "GCEZWKCA5VLDNRLN3RPRJMRZOX3Z6G5CHCGSNFHEYVXM3XOJMDS674JZ"
	futureTime := time.Now().Add(24 * time.Hour)
	pastTime := time.Now().Add(-1 * time.Hour)

	tests := []struct {
		name    string
		req     CreateMarketRequest
		wantErr error
	}{
		{
			name: "valid request",
			req: CreateMarketRequest{
				Question:        "Will BTC reach $100k?",
				Description:     "Test description",
				CloseTime:       futureTime,
				LiquidityParam:  100,
				OraclePublicKey: validKey,
			},
			wantErr: nil,
		},
		{
			name: "empty question",
			req: CreateMarketRequest{
				Question:        "",
				CloseTime:       futureTime,
				LiquidityParam:  100,
				OraclePublicKey: validKey,
			},
			wantErr: ErrEmptyQuestion,
		},
		{
			name: "whitespace only question",
			req: CreateMarketRequest{
				Question:        "   ",
				CloseTime:       futureTime,
				LiquidityParam:  100,
				OraclePublicKey: validKey,
			},
			wantErr: ErrEmptyQuestion,
		},
		{
			name: "question too long",
			req: CreateMarketRequest{
				Question:        strings.Repeat("x", MaxQuestionLength+1),
				CloseTime:       futureTime,
				LiquidityParam:  100,
				OraclePublicKey: validKey,
			},
			wantErr: ErrQuestionTooLong,
		},
		{
			name: "description too long",
			req: CreateMarketRequest{
				Question:        "Valid question?",
				Description:     strings.Repeat("x", MaxDescriptionLength+1),
				CloseTime:       futureTime,
				LiquidityParam:  100,
				OraclePublicKey: validKey,
			},
			wantErr: ErrDescriptionTooLong,
		},
		{
			name: "zero liquidity",
			req: CreateMarketRequest{
				Question:        "Valid question?",
				CloseTime:       futureTime,
				LiquidityParam:  0,
				OraclePublicKey: validKey,
			},
			wantErr: ErrInvalidLiquidityParam,
		},
		{
			name: "negative liquidity",
			req: CreateMarketRequest{
				Question:        "Valid question?",
				CloseTime:       futureTime,
				LiquidityParam:  -10,
				OraclePublicKey: validKey,
			},
			wantErr: ErrInvalidLiquidityParam,
		},
		{
			name: "past close time",
			req: CreateMarketRequest{
				Question:        "Valid question?",
				CloseTime:       pastTime,
				LiquidityParam:  100,
				OraclePublicKey: validKey,
			},
			wantErr: ErrCloseTimeInPast,
		},
		{
			name: "invalid public key",
			req: CreateMarketRequest{
				Question:        "Valid question?",
				CloseTime:       futureTime,
				LiquidityParam:  100,
				OraclePublicKey: "short",
			},
			wantErr: ErrInvalidPublicKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if err != tt.wantErr {
				t.Errorf("CreateMarketRequest.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuyRequest_Validate(t *testing.T) {
	validKey := "GCEZWKCA5VLDNRLN3RPRJMRZOX3Z6G5CHCGSNFHEYVXM3XOJMDS674JZ"

	tests := []struct {
		name    string
		req     BuyRequest
		wantErr error
	}{
		{
			name: "valid request",
			req: BuyRequest{
				UserPublicKey: validKey,
				MarketID:      validKey,
				Outcome:       OutcomeYes,
				ShareAmount:   10,
				Slippage:      0.01,
			},
			wantErr: nil,
		},
		{
			name: "valid request with max slippage",
			req: BuyRequest{
				UserPublicKey: validKey,
				MarketID:      validKey,
				Outcome:       OutcomeNo,
				ShareAmount:   100,
				Slippage:      MaxSlippage,
			},
			wantErr: nil,
		},
		{
			name: "invalid user public key",
			req: BuyRequest{
				UserPublicKey: "invalid",
				MarketID:      validKey,
				Outcome:       OutcomeYes,
				ShareAmount:   10,
				Slippage:      0.01,
			},
			wantErr: ErrInvalidPublicKey,
		},
		{
			name: "invalid market ID",
			req: BuyRequest{
				UserPublicKey: validKey,
				MarketID:      "invalid",
				Outcome:       OutcomeYes,
				ShareAmount:   10,
				Slippage:      0.01,
			},
			wantErr: ErrInvalidPublicKey,
		},
		{
			name: "invalid outcome",
			req: BuyRequest{
				UserPublicKey: validKey,
				MarketID:      validKey,
				Outcome:       "MAYBE",
				ShareAmount:   10,
				Slippage:      0.01,
			},
			wantErr: ErrInvalidOutcome,
		},
		{
			name: "empty outcome",
			req: BuyRequest{
				UserPublicKey: validKey,
				MarketID:      validKey,
				Outcome:       "",
				ShareAmount:   10,
				Slippage:      0.01,
			},
			wantErr: ErrInvalidOutcome,
		},
		{
			name: "zero share amount",
			req: BuyRequest{
				UserPublicKey: validKey,
				MarketID:      validKey,
				Outcome:       OutcomeYes,
				ShareAmount:   0,
				Slippage:      0.01,
			},
			wantErr: ErrInvalidShareAmount,
		},
		{
			name: "negative share amount",
			req: BuyRequest{
				UserPublicKey: validKey,
				MarketID:      validKey,
				Outcome:       OutcomeYes,
				ShareAmount:   -10,
				Slippage:      0.01,
			},
			wantErr: ErrInvalidShareAmount,
		},
		{
			name: "zero slippage",
			req: BuyRequest{
				UserPublicKey: validKey,
				MarketID:      validKey,
				Outcome:       OutcomeYes,
				ShareAmount:   10,
				Slippage:      0,
			},
			wantErr: ErrInvalidSlippage,
		},
		{
			name: "negative slippage",
			req: BuyRequest{
				UserPublicKey: validKey,
				MarketID:      validKey,
				Outcome:       OutcomeYes,
				ShareAmount:   10,
				Slippage:      -0.01,
			},
			wantErr: ErrInvalidSlippage,
		},
		{
			name: "slippage too high",
			req: BuyRequest{
				UserPublicKey: validKey,
				MarketID:      validKey,
				Outcome:       OutcomeYes,
				ShareAmount:   10,
				Slippage:      0.11, // > MaxSlippage
			},
			wantErr: ErrInvalidSlippage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if err != tt.wantErr {
				t.Errorf("BuyRequest.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolveRequest_Validate(t *testing.T) {
	validKey := "GCEZWKCA5VLDNRLN3RPRJMRZOX3Z6G5CHCGSNFHEYVXM3XOJMDS674JZ"

	tests := []struct {
		name    string
		req     ResolveRequest
		wantErr error
	}{
		{
			name: "valid YES resolution",
			req: ResolveRequest{
				MarketID:        validKey,
				WinningOutcome:  OutcomeYes,
				OraclePublicKey: validKey,
			},
			wantErr: nil,
		},
		{
			name: "valid NO resolution",
			req: ResolveRequest{
				MarketID:        validKey,
				WinningOutcome:  OutcomeNo,
				OraclePublicKey: validKey,
			},
			wantErr: nil,
		},
		{
			name: "invalid market ID",
			req: ResolveRequest{
				MarketID:        "invalid",
				WinningOutcome:  OutcomeYes,
				OraclePublicKey: validKey,
			},
			wantErr: ErrInvalidPublicKey,
		},
		{
			name: "invalid winning outcome",
			req: ResolveRequest{
				MarketID:        validKey,
				WinningOutcome:  "MAYBE",
				OraclePublicKey: validKey,
			},
			wantErr: ErrInvalidOutcome,
		},
		{
			name: "empty winning outcome",
			req: ResolveRequest{
				MarketID:        validKey,
				WinningOutcome:  "",
				OraclePublicKey: validKey,
			},
			wantErr: ErrInvalidOutcome,
		},
		{
			name: "invalid oracle public key",
			req: ResolveRequest{
				MarketID:        validKey,
				WinningOutcome:  OutcomeYes,
				OraclePublicKey: "invalid",
			},
			wantErr: ErrInvalidPublicKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if err != tt.wantErr {
				t.Errorf("ResolveRequest.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMarket_IsResolved(t *testing.T) {
	tests := []struct {
		name       string
		resolution Outcome
		want       bool
	}{
		{"unresolved (empty)", "", false},
		{"resolved YES", OutcomeYes, true},
		{"resolved NO", OutcomeNo, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Market{Resolution: tt.resolution}
			if got := m.IsResolved(); got != tt.want {
				t.Errorf("Market.IsResolved() = %v, want %v", got, tt.want)
			}
		})
	}
}
