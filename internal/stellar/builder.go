package stellar

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"

	"github.com/mtlprog/total/internal/config"
	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/txnbuild"
)

// TransactionTimeout is the default timeout for transactions in seconds.
// Shorter timeout (2 minutes) prevents stale transactions from being valid too long.
const TransactionTimeout = 120

// Builder creates Stellar transactions for market operations.
type Builder struct {
	client            Client
	networkPassphrase string
	baseFee           int64
	oracleKeypair     *keypair.Full // Oracle keypair for signing market operations
}

// NewBuilder creates a new transaction builder.
func NewBuilder(client Client, networkPassphrase string, baseFee int64, oracleSecret string) (*Builder, error) {
	var oracleKP *keypair.Full
	if oracleSecret != "" {
		kp, err := keypair.ParseFull(oracleSecret)
		if err != nil {
			return nil, fmt.Errorf("invalid oracle secret key: %w", err)
		}
		oracleKP = kp
	}
	return &Builder{
		client:            client,
		networkPassphrase: networkPassphrase,
		baseFee:           baseFee,
		oracleKeypair:     oracleKP,
	}, nil
}

// CreateMarketTxParams contains parameters for creating a market.
type CreateMarketTxParams struct {
	OraclePublicKey string
	MarketKeypair   *keypair.Full // New market account keypair
	MetadataHash    string        // IPFS hash
	LiquidityParam  float64       // LMSR b parameter
	InitialFunding  float64       // EURMTL to fund the market
}

// BuildCreateMarketTx builds a transaction to create a new prediction market.
// The transaction creates a new account, sets up YES/NO tokens, and stores metadata.
// Returns XDR that must be signed by the oracle.
func (b *Builder) BuildCreateMarketTx(ctx context.Context, params CreateMarketTxParams) (string, error) {
	// Get oracle account for sequence number
	oracleAccount, err := b.client.GetAccount(ctx, params.OraclePublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get oracle account: %w", err)
	}

	marketAddress := params.MarketKeypair.Address()

	// Generate asset codes based on market address (first 4 chars)
	yesCode := "YES" + marketAddress[:4]
	noCode := "NO" + marketAddress[:4]

	eurmtl := txnbuild.CreditAsset{
		Code:   config.EURMTLCode,
		Issuer: config.EURMTLIssuer,
	}

	// Note: YES/NO assets are issued by the market account itself
	// They will be created when market account sends them via Payment operations

	// Build operations:
	// 1. Create market account (sponsored by oracle)
	// 2. Market account trusts EURMTL
	// 3. Store IPFS metadata hash
	// 4. Store liquidity parameter
	// 5. Fund market with EURMTL

	ops := []txnbuild.Operation{
		// Begin sponsoring the market account
		&txnbuild.BeginSponsoringFutureReserves{
			SponsoredID: marketAddress,
		},
		// Create market account with minimum balance
		&txnbuild.CreateAccount{
			Destination: marketAddress,
			Amount:      fmt.Sprintf("%.7f", config.MarketAccountMinReserve),
		},
		// Market trusts EURMTL
		&txnbuild.ChangeTrust{
			SourceAccount: marketAddress,
			Line:          eurmtl.MustToChangeTrustAsset(),
			Limit:         txnbuild.MaxTrustlineLimit,
		},
		// Store metadata hash
		&txnbuild.ManageData{
			SourceAccount: marketAddress,
			Name:          "ipfs",
			Value:         []byte(params.MetadataHash),
		},
		// Store liquidity parameter
		&txnbuild.ManageData{
			SourceAccount: marketAddress,
			Name:          "b",
			Value:         []byte(strconv.FormatFloat(params.LiquidityParam, 'f', 2, 64)),
		},
		// Store YES asset code
		&txnbuild.ManageData{
			SourceAccount: marketAddress,
			Name:          "yes",
			Value:         []byte(yesCode),
		},
		// Store NO asset code
		&txnbuild.ManageData{
			SourceAccount: marketAddress,
			Name:          "no",
			Value:         []byte(noCode),
		},
		// Add oracle as signer on market account (for signing buy/resolve operations)
		&txnbuild.SetOptions{
			SourceAccount: marketAddress,
			Signer: &txnbuild.Signer{
				Address: params.OraclePublicKey,
				Weight:  1,
			},
			MasterWeight:    txnbuild.NewThreshold(0), // Disable master key
			LowThreshold:    txnbuild.NewThreshold(1),
			MediumThreshold: txnbuild.NewThreshold(1),
			HighThreshold:   txnbuild.NewThreshold(1),
		},
		// End sponsorship
		&txnbuild.EndSponsoringFutureReserves{
			SourceAccount: marketAddress,
		},
		// Fund market with EURMTL (for initial liquidity)
		&txnbuild.Payment{
			Destination: marketAddress,
			Amount:      fmt.Sprintf("%.7f", params.InitialFunding),
			Asset:       eurmtl,
		},
	}

	tx, err := txnbuild.NewTransaction(
		txnbuild.TransactionParams{
			SourceAccount:        oracleAccount,
			IncrementSequenceNum: true,
			Operations:           ops,
			BaseFee:              b.baseFee,
			Preconditions: txnbuild.Preconditions{
				TimeBounds: txnbuild.NewTimeout(TransactionTimeout),
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to build transaction: %w", err)
	}

	// Sign with market keypair (required for sponsored operations)
	tx, err = tx.Sign(b.networkPassphrase, params.MarketKeypair)
	if err != nil {
		return "", fmt.Errorf("failed to sign with market key: %w", err)
	}

	// Encode to XDR - oracle still needs to sign
	xdr, err := tx.Base64()
	if err != nil {
		return "", fmt.Errorf("failed to encode transaction: %w", err)
	}

	return xdr, nil
}

// BuyTokenTxParams contains parameters for buying outcome tokens.
type BuyTokenTxParams struct {
	UserPublicKey   string
	MarketPublicKey string
	Outcome         string  // "YES" or "NO"
	TokenAmount     float64 // Tokens to receive
	MaxCost         float64 // Maximum EURMTL to pay
}

// BuildBuyTokenTx builds a transaction for a user to buy outcome tokens.
// The market acts as a "virtual" market maker - tokens are sent from market to user.
// Returns XDR that must be signed by the user.
func (b *Builder) BuildBuyTokenTx(ctx context.Context, params BuyTokenTxParams) (string, error) {
	userAccount, err := b.client.GetAccount(ctx, params.UserPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get user account: %w", err)
	}

	// Get market data to find asset codes
	marketData, err := b.client.GetAccountData(ctx, params.MarketPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get market data: %w", err)
	}

	assetCode, err := b.decodeAssetCode(marketData, params.Outcome)
	if err != nil {
		return "", err
	}

	eurmtl := txnbuild.CreditAsset{
		Code:   config.EURMTLCode,
		Issuer: config.EURMTLIssuer,
	}

	outcomeAsset := txnbuild.CreditAsset{
		Code:   assetCode,
		Issuer: params.MarketPublicKey,
	}

	ops := []txnbuild.Operation{
		// User establishes trustline to outcome token (if not already)
		&txnbuild.ChangeTrust{
			Line:  outcomeAsset.MustToChangeTrustAsset(),
			Limit: txnbuild.MaxTrustlineLimit,
		},
		// User pays EURMTL to market
		&txnbuild.Payment{
			Destination: params.MarketPublicKey,
			Amount:      fmt.Sprintf("%.7f", params.MaxCost),
			Asset:       eurmtl,
		},
		// Market sends outcome tokens to user
		// Note: Market signs this operation separately
		&txnbuild.Payment{
			SourceAccount: params.MarketPublicKey,
			Destination:   params.UserPublicKey,
			Amount:        fmt.Sprintf("%.7f", params.TokenAmount),
			Asset:         outcomeAsset,
		},
	}

	tx, err := txnbuild.NewTransaction(
		txnbuild.TransactionParams{
			SourceAccount:        userAccount,
			IncrementSequenceNum: true,
			Operations:           ops,
			BaseFee:              b.baseFee,
			Preconditions: txnbuild.Preconditions{
				TimeBounds: txnbuild.NewTimeout(TransactionTimeout),
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to build transaction: %w", err)
	}

	xdr, err := tx.Base64()
	if err != nil {
		return "", fmt.Errorf("failed to encode transaction: %w", err)
	}

	return xdr, nil
}

// ResolveTxParams contains parameters for resolving a market.
type ResolveTxParams struct {
	OraclePublicKey string
	MarketPublicKey string
	WinningOutcome  string // "YES" or "NO"
}

// BuildResolveTx builds a transaction to resolve a market.
// This marks the market as resolved - distribution happens in a separate step.
// Returns XDR that must be signed by the oracle.
func (b *Builder) BuildResolveTx(ctx context.Context, params ResolveTxParams) (string, error) {
	oracleAccount, err := b.client.GetAccount(ctx, params.OraclePublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get oracle account: %w", err)
	}

	if params.WinningOutcome != "YES" && params.WinningOutcome != "NO" {
		return "", fmt.Errorf("invalid winning outcome: %s", params.WinningOutcome)
	}

	ops := []txnbuild.Operation{
		// Store resolution result on market account
		// Note: Oracle must have signing authority on market account
		&txnbuild.ManageData{
			SourceAccount: params.MarketPublicKey,
			Name:          "resolution",
			Value:         []byte(params.WinningOutcome),
		},
	}

	tx, err := txnbuild.NewTransaction(
		txnbuild.TransactionParams{
			SourceAccount:        oracleAccount,
			IncrementSequenceNum: true,
			Operations:           ops,
			BaseFee:              b.baseFee,
			Preconditions: txnbuild.Preconditions{
				TimeBounds: txnbuild.NewTimeout(TransactionTimeout),
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to build transaction: %w", err)
	}

	xdr, err := tx.Base64()
	if err != nil {
		return "", fmt.Errorf("failed to encode transaction: %w", err)
	}

	return xdr, nil
}

// ClaimWinningsTxParams contains parameters for claiming winnings.
type ClaimWinningsTxParams struct {
	UserPublicKey   string
	MarketPublicKey string
	WinningOutcome  string  // "YES" or "NO"
	TokenAmount     float64 // Amount of winning tokens to redeem
}

// BuildClaimWinningsTx builds a transaction for a user to claim their winnings.
// User sends winning tokens to market and receives EURMTL in return.
// Returns XDR that must be signed by user and market.
func (b *Builder) BuildClaimWinningsTx(ctx context.Context, params ClaimWinningsTxParams) (string, error) {
	userAccount, err := b.client.GetAccount(ctx, params.UserPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get user account: %w", err)
	}

	// Get market data to find asset codes
	marketData, err := b.client.GetAccountData(ctx, params.MarketPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get market data: %w", err)
	}

	assetCode, err := b.decodeAssetCode(marketData, params.WinningOutcome)
	if err != nil {
		return "", err
	}

	eurmtl := txnbuild.CreditAsset{
		Code:   config.EURMTLCode,
		Issuer: config.EURMTLIssuer,
	}

	winningAsset := txnbuild.CreditAsset{
		Code:   assetCode,
		Issuer: params.MarketPublicKey,
	}

	// Each winning token is redeemable for 1 EURMTL
	ops := []txnbuild.Operation{
		// User sends winning tokens back to market (burns them)
		&txnbuild.Payment{
			Destination: params.MarketPublicKey,
			Amount:      fmt.Sprintf("%.7f", params.TokenAmount),
			Asset:       winningAsset,
		},
		// Market sends EURMTL to user
		&txnbuild.Payment{
			SourceAccount: params.MarketPublicKey,
			Destination:   params.UserPublicKey,
			Amount:        fmt.Sprintf("%.7f", params.TokenAmount), // 1:1 redemption
			Asset:         eurmtl,
		},
	}

	tx, err := txnbuild.NewTransaction(
		txnbuild.TransactionParams{
			SourceAccount:        userAccount,
			IncrementSequenceNum: true,
			Operations:           ops,
			BaseFee:              b.baseFee,
			Preconditions: txnbuild.Preconditions{
				TimeBounds: txnbuild.NewTimeout(TransactionTimeout),
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to build transaction: %w", err)
	}

	xdr, err := tx.Base64()
	if err != nil {
		return "", fmt.Errorf("failed to encode transaction: %w", err)
	}

	return xdr, nil
}

// decodeAssetCode decodes the asset code from market data for the given outcome.
func (b *Builder) decodeAssetCode(marketData map[string]string, outcome string) (string, error) {
	var dataKey string
	switch outcome {
	case "YES":
		dataKey = "yes"
	case "NO":
		dataKey = "no"
	default:
		return "", fmt.Errorf("invalid outcome: %s", outcome)
	}

	encoded, ok := marketData[dataKey]
	if !ok {
		return "", fmt.Errorf("market data missing %s asset code", outcome)
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode %s asset code: %w", outcome, err)
	}

	if len(decoded) == 0 {
		return "", fmt.Errorf("%s asset code is empty", outcome)
	}

	return string(decoded), nil
}
