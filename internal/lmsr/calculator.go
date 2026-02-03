package lmsr

import (
	"errors"
	"math"
)

var (
	ErrInvalidOutcome     = errors.New("invalid outcome: must be YES or NO")
	ErrNegativeAmount     = errors.New("amount must be positive")
	ErrInvalidLiquidity   = errors.New("liquidity parameter must be positive")
	ErrNegativeQuantities = errors.New("quantities must be non-negative")
	ErrInsufficientTokens = errors.New("cannot sell more than available")
)

// Calculator implements LMSR (Logarithmic Market Scoring Rule) pricing.
// Reference: https://gnosis-pm-js.readthedocs.io/en/v1.3.0/lmsr-primer.html
type Calculator struct {
	b float64 // Liquidity parameter
}

// New creates a new LMSR calculator with the given liquidity parameter.
// The b parameter controls market depth: larger b = more liquidity, smaller price impact.
func New(liquidityParam float64) (*Calculator, error) {
	if liquidityParam <= 0 {
		return nil, ErrInvalidLiquidity
	}
	return &Calculator{b: liquidityParam}, nil
}

// cost calculates the cost function C(q) = b * ln(exp(qYes/b) + exp(qNo/b))
func (c *Calculator) cost(qYes, qNo float64) float64 {
	// Use log-sum-exp trick for numerical stability
	maxQ := math.Max(qYes, qNo)
	return c.b * (maxQ/c.b + math.Log(math.Exp((qYes-maxQ)/c.b)+math.Exp((qNo-maxQ)/c.b)))
}

// Price calculates current prices for YES and NO outcomes.
// Returns (priceYes, priceNo) where both sum to 1.
// Price represents the probability of each outcome.
func (c *Calculator) Price(qYes, qNo float64) (priceYes, priceNo float64, err error) {
	if qYes < 0 || qNo < 0 {
		return 0, 0, ErrNegativeQuantities
	}

	// P(yes) = exp(qYes/b) / (exp(qYes/b) + exp(qNo/b))
	// Use log-sum-exp trick for numerical stability
	expYes := math.Exp(qYes / c.b)
	expNo := math.Exp(qNo / c.b)
	sum := expYes + expNo

	priceYes = expYes / sum
	priceNo = expNo / sum

	return priceYes, priceNo, nil
}

// CalculateCost calculates the cost to buy a given amount of outcome tokens.
// Returns the cost in collateral tokens.
func (c *Calculator) CalculateCost(qYes, qNo, amount float64, outcome string) (float64, error) {
	if amount <= 0 {
		return 0, ErrNegativeAmount
	}
	if qYes < 0 || qNo < 0 {
		return 0, ErrNegativeQuantities
	}

	costBefore := c.cost(qYes, qNo)
	var costAfter float64

	switch outcome {
	case "YES":
		costAfter = c.cost(qYes+amount, qNo)
	case "NO":
		costAfter = c.cost(qYes, qNo+amount)
	default:
		return 0, ErrInvalidOutcome
	}

	return costAfter - costBefore, nil
}

// CalculateSellReturn calculates the return from selling outcome tokens.
// Returns the amount of collateral received.
func (c *Calculator) CalculateSellReturn(qYes, qNo, amount float64, outcome string) (float64, error) {
	if amount <= 0 {
		return 0, ErrNegativeAmount
	}
	if qYes < 0 || qNo < 0 {
		return 0, ErrNegativeQuantities
	}

	costBefore := c.cost(qYes, qNo)
	var costAfter float64

	switch outcome {
	case "YES":
		if qYes < amount {
			return 0, ErrInsufficientTokens
		}
		costAfter = c.cost(qYes-amount, qNo)
	case "NO":
		if qNo < amount {
			return 0, ErrInsufficientTokens
		}
		costAfter = c.cost(qYes, qNo-amount)
	default:
		return 0, ErrInvalidOutcome
	}

	return costBefore - costAfter, nil
}

// InitialLiquidity calculates the initial funding required for a binary market.
// This is the maximum possible loss for the market maker: b * ln(2)
func (c *Calculator) InitialLiquidity() float64 {
	return c.b * math.Log(2)
}

// Quote calculates a complete price quote for buying tokens.
func (c *Calculator) Quote(qYes, qNo, amount float64, outcome string) (cost, pricePerShare, newProbability float64, err error) {
	cost, err = c.CalculateCost(qYes, qNo, amount, outcome)
	if err != nil {
		return 0, 0, 0, err
	}

	pricePerShare = cost / amount

	// Calculate new probability after purchase
	var newQYes, newQNo float64
	switch outcome {
	case "YES":
		newQYes, newQNo = qYes+amount, qNo
	case "NO":
		newQYes, newQNo = qYes, qNo+amount
	}

	newPriceYes, _, err := c.Price(newQYes, newQNo)
	if err != nil {
		return 0, 0, 0, err
	}

	if outcome == "YES" {
		newProbability = newPriceYes
	} else {
		newProbability = 1 - newPriceYes
	}

	return cost, pricePerShare, newProbability, nil
}

// LiquidityParam returns the liquidity parameter b.
func (c *Calculator) LiquidityParam() float64 {
	return c.b
}
