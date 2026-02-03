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

func TestMarket_Validate(t *testing.T) {
	now := time.Now()

	validMarket := func() Market {
		return Market{
			ID:             "CTEST123456789012345678901234567890123456789012345678",
			LiquidityParam: 100,
			YesSold:        10,
			NoSold:         5,
			PriceYes:       0.6,
			PriceNo:        0.4,
		}
	}

	tests := []struct {
		name    string
		modify  func(*Market)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid market",
			modify:  func(m *Market) {},
			wantErr: false,
		},
		{
			name:    "empty ID",
			modify:  func(m *Market) { m.ID = "" },
			wantErr: true,
			errMsg:  "market ID is required",
		},
		{
			name:    "zero liquidity param",
			modify:  func(m *Market) { m.LiquidityParam = 0 },
			wantErr: true,
			errMsg:  "liquidity parameter must be positive",
		},
		{
			name:    "negative liquidity param",
			modify:  func(m *Market) { m.LiquidityParam = -10 },
			wantErr: true,
			errMsg:  "liquidity parameter must be positive",
		},
		{
			name:    "negative yes_sold",
			modify:  func(m *Market) { m.YesSold = -1 },
			wantErr: true,
			errMsg:  "yes_sold must be non-negative",
		},
		{
			name:    "negative no_sold",
			modify:  func(m *Market) { m.NoSold = -1 },
			wantErr: true,
			errMsg:  "no_sold must be non-negative",
		},
		{
			name:    "price_yes below zero",
			modify:  func(m *Market) { m.PriceYes = -0.1 },
			wantErr: true,
			errMsg:  "price_yes must be in range [0, 1]",
		},
		{
			name:    "price_yes above one",
			modify:  func(m *Market) { m.PriceYes = 1.5 },
			wantErr: true,
			errMsg:  "price_yes must be in range [0, 1]",
		},
		{
			name:    "price_no below zero",
			modify:  func(m *Market) { m.PriceNo = -0.1 },
			wantErr: true,
			errMsg:  "price_no must be in range [0, 1]",
		},
		{
			name:    "price_no above one",
			modify:  func(m *Market) { m.PriceNo = 1.5 },
			wantErr: true,
			errMsg:  "price_no must be in range [0, 1]",
		},
		{
			name:    "invalid resolution outcome",
			modify:  func(m *Market) { m.Resolution = "INVALID" },
			wantErr: true,
			errMsg:  "invalid outcome",
		},
		{
			name: "resolved without timestamp",
			modify: func(m *Market) {
				m.Resolution = OutcomeYes
				m.ResolvedAt = nil
			},
			wantErr: true,
			errMsg:  "resolved market must have resolved_at timestamp",
		},
		{
			name: "timestamp without resolution",
			modify: func(m *Market) {
				m.Resolution = ""
				m.ResolvedAt = &now
			},
			wantErr: true,
			errMsg:  "unresolved market must not have resolved_at timestamp",
		},
		{
			name: "valid resolved market",
			modify: func(m *Market) {
				m.Resolution = OutcomeYes
				m.ResolvedAt = &now
			},
			wantErr: false,
		},
		{
			name:    "price_yes at boundary 0",
			modify:  func(m *Market) { m.PriceYes = 0 },
			wantErr: false,
		},
		{
			name:    "price_yes at boundary 1",
			modify:  func(m *Market) { m.PriceYes = 1 },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validMarket()
			tt.modify(&m)
			err := m.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Market.Validate() expected error, got nil")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Market.Validate() error = %q, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Market.Validate() unexpected error: %v", err)
				}
			}
		})
	}
}
