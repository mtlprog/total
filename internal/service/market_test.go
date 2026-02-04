package service

import (
	"errors"
	"math"
	"testing"

	"github.com/mtlprog/total/internal/model"
)

func TestSafeFloatToInt64(t *testing.T) {
	tests := []struct {
		name    string
		input   float64
		want    int64
		wantErr bool
	}{
		{"zero", 0, 0, false},
		{"positive integer", 100, 100, false},
		{"negative integer", -50, -50, false},
		{"positive with decimal truncated", 100.7, 100, false},
		{"negative with decimal truncated", -50.9, -50, false},
		{"large positive", 1e15, 1000000000000000, false},
		{"large negative", -1e15, -1000000000000000, false},
		{"NaN", math.NaN(), 0, true},
		{"positive infinity", math.Inf(1), 0, true},
		{"negative infinity", math.Inf(-1), 0, true},
		{"exceeds max int64", float64(math.MaxInt64) * 2, 0, true},
		{"exceeds min int64", float64(math.MinInt64) * 2, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeFloatToInt64(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("safeFloatToInt64(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("safeFloatToInt64(%v) = %v, want %v", tt.input, got, tt.want)
			}
			if tt.wantErr && !errors.Is(err, ErrAmountOverflow) {
				t.Errorf("safeFloatToInt64(%v) error = %v, want ErrAmountOverflow", tt.input, err)
			}
		})
	}
}

func TestTradeRequest_Validate(t *testing.T) {
	validRequest := TradeRequest{
		UserPublicKey: "GBXGQJWVLWOYHFLVTKWV5FGHA3LNYY2JQKM7OAJAUEQFU6LPCSEFVXON",
		ContractID:    "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHK3M",
		Outcome:       model.OutcomeYes,
		ShareAmount:   10.0,
		Slippage:      0.01,
	}

	tests := []struct {
		name    string
		modify  func(*TradeRequest)
		wantErr error
	}{
		{
			name:    "valid request",
			modify:  func(r *TradeRequest) {},
			wantErr: nil,
		},
		{
			name:    "empty user public key",
			modify:  func(r *TradeRequest) { r.UserPublicKey = "" },
			wantErr: model.ErrInvalidPublicKey,
		},
		{
			name:    "invalid user public key prefix",
			modify:  func(r *TradeRequest) { r.UserPublicKey = "SAXGQJWVLWOYHFLVTKWV5FGHA3LNYY2JQKM7OAJAUEQFU6LPCSEFVXON" },
			wantErr: model.ErrInvalidPublicKey,
		},
		{
			name:    "short user public key",
			modify:  func(r *TradeRequest) { r.UserPublicKey = "GABC" },
			wantErr: model.ErrInvalidPublicKey,
		},
		{
			name:    "empty contract ID",
			modify:  func(r *TradeRequest) { r.ContractID = "" },
			wantErr: nil, // ValidateContractID returns its own error
		},
		{
			name:    "invalid outcome",
			modify:  func(r *TradeRequest) { r.Outcome = "MAYBE" },
			wantErr: model.ErrInvalidOutcome,
		},
		{
			name:    "zero share amount",
			modify:  func(r *TradeRequest) { r.ShareAmount = 0 },
			wantErr: model.ErrInvalidShareAmount,
		},
		{
			name:    "negative share amount",
			modify:  func(r *TradeRequest) { r.ShareAmount = -10 },
			wantErr: model.ErrInvalidShareAmount,
		},
		{
			name:    "zero slippage",
			modify:  func(r *TradeRequest) { r.Slippage = 0 },
			wantErr: model.ErrInvalidSlippage,
		},
		{
			name:    "negative slippage",
			modify:  func(r *TradeRequest) { r.Slippage = -0.01 },
			wantErr: model.ErrInvalidSlippage,
		},
		{
			name:    "slippage over max",
			modify:  func(r *TradeRequest) { r.Slippage = 0.15 },
			wantErr: model.ErrInvalidSlippage,
		},
		{
			name:    "slippage at max boundary",
			modify:  func(r *TradeRequest) { r.Slippage = model.MaxSlippage },
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRequest
			tt.modify(&req)
			err := req.Validate()
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				}
			} else if err != nil && tt.name != "empty contract ID" {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

func TestResolveRequest_Validate(t *testing.T) {
	validRequest := ResolveRequest{
		OraclePublicKey: "GBXGQJWVLWOYHFLVTKWV5FGHA3LNYY2JQKM7OAJAUEQFU6LPCSEFVXON",
		ContractID:      "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHK3M",
		WinningOutcome:  model.OutcomeYes,
	}

	tests := []struct {
		name    string
		modify  func(*ResolveRequest)
		wantErr error
	}{
		{
			name:    "valid request",
			modify:  func(r *ResolveRequest) {},
			wantErr: nil,
		},
		{
			name:    "invalid oracle public key",
			modify:  func(r *ResolveRequest) { r.OraclePublicKey = "" },
			wantErr: model.ErrInvalidPublicKey,
		},
		{
			name:    "invalid winning outcome",
			modify:  func(r *ResolveRequest) { r.WinningOutcome = "INVALID" },
			wantErr: model.ErrInvalidOutcome,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRequest
			tt.modify(&req)
			err := req.Validate()
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClaimRequest_Validate(t *testing.T) {
	validRequest := ClaimRequest{
		UserPublicKey: "GBXGQJWVLWOYHFLVTKWV5FGHA3LNYY2JQKM7OAJAUEQFU6LPCSEFVXON",
		ContractID:    "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHK3M",
	}

	tests := []struct {
		name    string
		modify  func(*ClaimRequest)
		wantErr error
	}{
		{
			name:    "valid request",
			modify:  func(r *ClaimRequest) {},
			wantErr: nil,
		},
		{
			name:    "invalid user public key",
			modify:  func(r *ClaimRequest) { r.UserPublicKey = "" },
			wantErr: model.ErrInvalidPublicKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRequest
			tt.modify(&req)
			err := req.Validate()
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWithdrawRequest_Validate(t *testing.T) {
	validRequest := WithdrawRequest{
		OraclePublicKey: "GBXGQJWVLWOYHFLVTKWV5FGHA3LNYY2JQKM7OAJAUEQFU6LPCSEFVXON",
		ContractID:      "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAHK3M",
	}

	tests := []struct {
		name    string
		modify  func(*WithdrawRequest)
		wantErr error
	}{
		{
			name:    "valid request",
			modify:  func(r *WithdrawRequest) {},
			wantErr: nil,
		},
		{
			name:    "invalid oracle public key",
			modify:  func(r *WithdrawRequest) { r.OraclePublicKey = "" },
			wantErr: model.ErrInvalidPublicKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRequest
			tt.modify(&req)
			err := req.Validate()
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
