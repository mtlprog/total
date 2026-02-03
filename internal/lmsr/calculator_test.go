package lmsr

import (
	"math"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name      string
		b         float64
		wantErr   bool
		errString string
	}{
		{"valid positive", 100, false, ""},
		{"valid small", 0.01, false, ""},
		{"zero", 0, true, "liquidity parameter must be positive"},
		{"negative", -10, true, "liquidity parameter must be positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc, err := New(tt.b)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if calc.LiquidityParam() != tt.b {
				t.Errorf("LiquidityParam() = %v, want %v", calc.LiquidityParam(), tt.b)
			}
		})
	}
}

func TestPrice(t *testing.T) {
	calc, _ := New(100)

	tests := []struct {
		name      string
		qYes      float64
		qNo       float64
		wantYes   float64
		wantNo    float64
		wantErr   bool
		tolerance float64
	}{
		{
			name: "equal quantities - 50/50",
			qYes: 0, qNo: 0,
			wantYes: 0.5, wantNo: 0.5,
			tolerance: 0.001,
		},
		{
			name: "more YES sold",
			qYes: 50, qNo: 0,
			wantYes: 0.622, wantNo: 0.378,
			tolerance: 0.01,
		},
		{
			name: "more NO sold",
			qYes: 0, qNo: 50,
			wantYes: 0.378, wantNo: 0.622,
			tolerance: 0.01,
		},
		{
			name: "negative qYes",
			qYes: -10, qNo: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priceYes, priceNo, err := calc.Price(tt.qYes, tt.qNo)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if math.Abs(priceYes-tt.wantYes) > tt.tolerance {
				t.Errorf("priceYes = %v, want %v (tolerance %v)", priceYes, tt.wantYes, tt.tolerance)
			}
			if math.Abs(priceNo-tt.wantNo) > tt.tolerance {
				t.Errorf("priceNo = %v, want %v (tolerance %v)", priceNo, tt.wantNo, tt.tolerance)
			}
			// Prices must sum to 1
			if math.Abs(priceYes+priceNo-1.0) > 0.0001 {
				t.Errorf("prices don't sum to 1: %v + %v = %v", priceYes, priceNo, priceYes+priceNo)
			}
		})
	}
}

func TestCalculateCost(t *testing.T) {
	calc, _ := New(100)

	tests := []struct {
		name      string
		qYes      float64
		qNo       float64
		amount    float64
		outcome   string
		wantCost  float64
		wantErr   bool
		tolerance float64
	}{
		{
			name: "buy 10 YES from 50/50",
			qYes: 0, qNo: 0,
			amount: 10, outcome: "YES",
			wantCost: 5.12, tolerance: 0.1,
		},
		{
			name: "buy 10 NO from 50/50",
			qYes: 0, qNo: 0,
			amount: 10, outcome: "NO",
			wantCost: 5.12, tolerance: 0.1,
		},
		{
			name: "buy YES when YES is already high",
			qYes: 100, qNo: 0,
			amount: 10, outcome: "YES",
			wantCost: 7.31, tolerance: 0.1,
		},
		{
			name: "invalid outcome",
			qYes: 0, qNo: 0,
			amount: 10, outcome: "MAYBE",
			wantErr: true,
		},
		{
			name: "negative amount",
			qYes: 0, qNo: 0,
			amount: -10, outcome: "YES",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost, err := calc.CalculateCost(tt.qYes, tt.qNo, tt.amount, tt.outcome)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if math.Abs(cost-tt.wantCost) > tt.tolerance {
				t.Errorf("cost = %v, want %v (tolerance %v)", cost, tt.wantCost, tt.tolerance)
			}
		})
	}
}

func TestCalculateSellReturn(t *testing.T) {
	calc, _ := New(100)

	tests := []struct {
		name       string
		qYes       float64
		qNo        float64
		amount     float64
		outcome    string
		wantReturn float64
		wantErr    error
		tolerance  float64
	}{
		{
			name: "sell 10 YES when market has 50 YES sold",
			qYes: 50, qNo: 0,
			amount: 10, outcome: "YES",
			wantReturn: 6.11, tolerance: 0.1,
		},
		{
			name: "sell 10 NO when market has 50 NO sold",
			qYes: 0, qNo: 50,
			amount: 10, outcome: "NO",
			wantReturn: 6.11, tolerance: 0.1,
		},
		{
			name: "sell all YES tokens",
			qYes: 100, qNo: 0,
			amount: 100, outcome: "YES",
			wantReturn: 62.01, tolerance: 0.5, // Larger tolerance for big amounts
		},
		{
			name: "sell more than available YES",
			qYes: 10, qNo: 0,
			amount: 20, outcome: "YES",
			wantErr: ErrInsufficientTokens,
		},
		{
			name: "sell more than available NO",
			qYes: 0, qNo: 10,
			amount: 20, outcome: "NO",
			wantErr: ErrInsufficientTokens,
		},
		{
			name: "invalid outcome",
			qYes: 50, qNo: 50,
			amount: 10, outcome: "MAYBE",
			wantErr: ErrInvalidOutcome,
		},
		{
			name: "negative amount",
			qYes: 50, qNo: 50,
			amount: -10, outcome: "YES",
			wantErr: ErrNegativeAmount,
		},
		{
			name: "zero amount",
			qYes: 50, qNo: 50,
			amount: 0, outcome: "YES",
			wantErr: ErrNegativeAmount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			returnVal, err := calc.CalculateSellReturn(tt.qYes, tt.qNo, tt.amount, tt.outcome)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if math.Abs(returnVal-tt.wantReturn) > tt.tolerance {
				t.Errorf("return = %v, want %v (tolerance %v)", returnVal, tt.wantReturn, tt.tolerance)
			}
		})
	}
}

func TestInitialLiquidity(t *testing.T) {
	tests := []struct {
		b        float64
		expected float64
	}{
		{100, 69.31},  // 100 * ln(2) ≈ 69.31
		{1000, 693.1}, // 1000 * ln(2) ≈ 693.1
		{10, 6.931},   // 10 * ln(2) ≈ 6.931
	}

	for _, tt := range tests {
		calc, _ := New(tt.b)
		result := calc.InitialLiquidity()
		if math.Abs(result-tt.expected) > 0.1 {
			t.Errorf("InitialLiquidity(b=%v) = %v, want %v", tt.b, result, tt.expected)
		}
	}
}

func TestQuoteYES(t *testing.T) {
	calc, _ := New(100)

	cost, pricePerShare, newProb, err := calc.Quote(0, 0, 10, "YES")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cost should be around 5.12 EURMTL for 10 YES tokens at 50/50
	if cost < 5 || cost > 6 {
		t.Errorf("cost = %v, expected ~5.12", cost)
	}

	// Price per share should be around 0.51
	if pricePerShare < 0.45 || pricePerShare > 0.55 {
		t.Errorf("pricePerShare = %v, expected ~0.51", pricePerShare)
	}

	// New probability should be higher than 50% after buying YES
	if newProb <= 0.5 {
		t.Errorf("newProb = %v, expected > 0.5", newProb)
	}
}

func TestQuoteNO(t *testing.T) {
	calc, _ := New(100)

	cost, pricePerShare, newProb, err := calc.Quote(0, 0, 10, "NO")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cost should be around 5.12 EURMTL for 10 NO tokens at 50/50 (symmetric)
	if cost < 5 || cost > 6 {
		t.Errorf("cost = %v, expected ~5.12", cost)
	}

	// Price per share should be around 0.51
	if pricePerShare < 0.45 || pricePerShare > 0.55 {
		t.Errorf("pricePerShare = %v, expected ~0.51", pricePerShare)
	}

	// New probability for NO should be higher than 50% after buying NO
	if newProb <= 0.5 {
		t.Errorf("newProb = %v, expected > 0.5", newProb)
	}

	// Verify that buying NO increases NO probability (which is 1 - YES probability)
	_, _, newYesProb, _ := calc.Quote(0, 0, 10, "YES")
	// newProb for NO should roughly equal newYesProb for YES due to symmetry
	if math.Abs(newProb-newYesProb) > 0.01 {
		t.Errorf("asymmetric quote: YES newProb=%v, NO newProb=%v", newYesProb, newProb)
	}
}

func TestPriceSymmetry(t *testing.T) {
	calc, _ := New(100)

	// Cost of buying YES should equal cost of buying NO at 50/50
	costYes, _ := calc.CalculateCost(0, 0, 10, "YES")
	costNo, _ := calc.CalculateCost(0, 0, 10, "NO")

	if math.Abs(costYes-costNo) > 0.0001 {
		t.Errorf("asymmetric pricing: YES=%v, NO=%v", costYes, costNo)
	}
}

func TestPriceImpact(t *testing.T) {
	calc, _ := New(100)

	// Larger purchases should have higher price impact (cost more per token)
	cost10, _ := calc.CalculateCost(0, 0, 10, "YES")
	cost100, _ := calc.CalculateCost(0, 0, 100, "YES")

	pricePerShare10 := cost10 / 10
	pricePerShare100 := cost100 / 100

	if pricePerShare100 <= pricePerShare10 {
		t.Errorf("larger purchase should have higher average price: 10=%v, 100=%v",
			pricePerShare10, pricePerShare100)
	}
}

func TestRoundTripInvariant(t *testing.T) {
	calc, _ := New(100)

	// Buy then sell same amount should result in small loss (spread)
	// Start at 50/50
	buyAmount := 10.0

	// Buy YES
	buyCost, _ := calc.CalculateCost(0, 0, buyAmount, "YES")

	// Now sell YES (market state after buy: qYes=10, qNo=0)
	sellReturn, _ := calc.CalculateSellReturn(buyAmount, 0, buyAmount, "YES")

	// Net cost should be positive (user loses money on round trip due to price impact)
	netCost := buyCost - sellReturn
	if netCost < 0 {
		t.Errorf("round trip should not be profitable: buyCost=%v, sellReturn=%v, net=%v",
			buyCost, sellReturn, netCost)
	}

	// But net cost should be small relative to transaction size
	if netCost > buyCost*0.2 { // Allow up to 20% loss on round trip
		t.Errorf("round trip loss too high: buyCost=%v, sellReturn=%v, loss=%v (%.1f%%)",
			buyCost, sellReturn, netCost, (netCost/buyCost)*100)
	}
}

func TestNumericalStabilityLargeQuantities(t *testing.T) {
	calc, _ := New(100)

	// Test with large quantities that could cause overflow
	largeQ := 10000.0

	priceYes, priceNo, err := calc.Price(largeQ, 0)
	if err != nil {
		t.Fatalf("unexpected error with large quantities: %v", err)
	}

	// Price should be very close to 1 for YES when it's heavily favored
	if priceYes < 0.99 {
		t.Errorf("priceYes with large quantity = %v, expected close to 1", priceYes)
	}
	if priceNo > 0.01 {
		t.Errorf("priceNo with large quantity = %v, expected close to 0", priceNo)
	}

	// Prices should still sum to 1
	if math.Abs(priceYes+priceNo-1.0) > 0.0001 {
		t.Errorf("prices don't sum to 1: %v + %v = %v", priceYes, priceNo, priceYes+priceNo)
	}
}

func TestNumericalStabilitySmallQuantities(t *testing.T) {
	calc, _ := New(100)

	// Test with very small quantities
	smallQ := 0.0001

	priceYes, priceNo, err := calc.Price(smallQ, 0)
	if err != nil {
		t.Fatalf("unexpected error with small quantities: %v", err)
	}

	// Should be very close to 50/50
	if math.Abs(priceYes-0.5) > 0.001 {
		t.Errorf("priceYes with tiny quantity = %v, expected ~0.5", priceYes)
	}
	if math.Abs(priceNo-0.5) > 0.001 {
		t.Errorf("priceNo with tiny quantity = %v, expected ~0.5", priceNo)
	}
}

func TestNumericalStabilitySmallLiquidity(t *testing.T) {
	// Very small liquidity parameter causes extreme price movements
	calc, err := New(0.001)
	if err != nil {
		t.Fatalf("failed to create calculator with small b: %v", err)
	}

	// Even small purchases should cause large price swings
	cost, err := calc.CalculateCost(0, 0, 1, "YES")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cost should be valid (not NaN or Inf)
	if math.IsNaN(cost) || math.IsInf(cost, 0) {
		t.Errorf("invalid cost with small liquidity: %v", cost)
	}

	// Price should move significantly
	priceYes, _, err := calc.Price(1, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if priceYes < 0.9 {
		t.Errorf("price should be very high with small liquidity: %v", priceYes)
	}
}

func TestNumericalStabilityLargeLiquidity(t *testing.T) {
	// Very large liquidity parameter causes minimal price movements
	calc, err := New(1000000)
	if err != nil {
		t.Fatalf("failed to create calculator with large b: %v", err)
	}

	// Large purchases should cause minimal price swings
	priceYesBefore, _, _ := calc.Price(0, 0)
	priceYesAfter, _, err := calc.Price(1000, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Price movement should be tiny
	priceMove := math.Abs(priceYesAfter - priceYesBefore)
	if priceMove > 0.01 {
		t.Errorf("price moved too much with large liquidity: %v -> %v (delta=%v)",
			priceYesBefore, priceYesAfter, priceMove)
	}
}

func TestAsymmetricMarketState(t *testing.T) {
	calc, _ := New(100)

	// Test extreme asymmetric states
	tests := []struct {
		name   string
		qYes   float64
		qNo    float64
		minYes float64
		maxYes float64
	}{
		{"heavy YES", 500, 0, 0.99, 1.0},
		{"heavy NO", 0, 500, 0.0, 0.01},
		{"moderate YES", 100, 50, 0.6, 0.8},
		{"moderate NO", 50, 100, 0.2, 0.4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priceYes, priceNo, err := calc.Price(tt.qYes, tt.qNo)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if priceYes < tt.minYes || priceYes > tt.maxYes {
				t.Errorf("priceYes = %v, expected in range [%v, %v]", priceYes, tt.minYes, tt.maxYes)
			}

			// Sanity check: prices sum to 1
			if math.Abs(priceYes+priceNo-1.0) > 0.0001 {
				t.Errorf("prices don't sum to 1: %v + %v", priceYes, priceNo)
			}
		})
	}
}
