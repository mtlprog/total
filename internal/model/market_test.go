package model

import (
	"strings"
	"testing"
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
