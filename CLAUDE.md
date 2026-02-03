# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Total

Stellar prediction market platform. Stateless web application that displays blockchain state and generates XDR transactions for users to sign externally.

## Commands

- `make build` - Build for local macOS
- `make run` - Run locally (requires ORACLE_PUBLIC_KEY env var)
- `make test` - Run tests
- `make lint` - Format and vet code

## Project Structure

```
cmd/total/         - CLI entry point
internal/
├── chart/         - ASCII price charts
├── config/        - Configuration constants (Stellar, IPFS)
├── handler/       - HTTP request handlers
├── ipfs/          - Pinata IPFS client
├── lmsr/          - LMSR pricing calculator
├── logger/        - Structured logging (slog/JSON)
├── model/         - Data structures (Market, Quote, etc.)
├── service/       - Business logic (MarketService)
├── stellar/       - Stellar client and transaction builder
└── template/      - HTML templates
```

## Stack

- Go 1.24+
- github.com/stellar/go-stellar-sdk (Horizon client, txnbuild)
- LMSR (Logarithmic Market Scoring Rule) for pricing
- Pinata for IPFS metadata storage
- No database - all state from Stellar blockchain

## Architecture

### Stateless Design
- No local database
- All market state read from Stellar Horizon API
- Site generates XDR transactions, users sign externally
- Market IDs passed via --market-ids flag or discovered at runtime

### LMSR Pricing
- Price formula: `P(yes) = e^(qYes/b) / (e^(qYes/b) + e^(qNo/b))`
- Cost function: `C(q) = b * ln(e^(qYes/b) + e^(qNo/b))`
- Parameter `b` controls liquidity depth
- Initial funding = `b * ln(2)` EURMTL

### Market Lifecycle
1. Oracle creates market (sponsored account, IPFS metadata)
2. Users buy outcome tokens (EURMTL → YES/NO tokens)
3. Oracle resolves market
4. Winners redeem tokens for EURMTL

## Environment Variables

- `ORACLE_PUBLIC_KEY` (required) - Stellar account that creates/resolves markets
- `HORIZON_URL` - Horizon API URL (default: mainnet)
- `NETWORK_PASSPHRASE` - Stellar network passphrase
- `PINATA_API_KEY` - Pinata API key for IPFS
- `PINATA_SECRET` - Pinata API secret
- `MARKET_IDS` - Comma-separated list of known market IDs
- `PORT` - HTTP server port (default: 8080)
- `LOG_LEVEL` - Log level (debug, info, warn, error)

## Gotchas

- `.gitignore`: use `/total` not `total` to avoid ignoring `cmd/total/`
- Stellar SDK moved from `stellar/go` to `stellar/go-stellar-sdk` (Dec 2025)
- Market accounts are sponsored by oracle (no XLM reserve needed for users)
- XDR transactions require external signing (Stellar Lab, Freighter, etc.)
- Market accounts use multisig: oracle added as signer via SetOptions, master key disabled
- Outcome tokens (YES/NO) issued by market account, oracle signs payment operations
- Use `errors.Is()` not `==` for error comparison (errors may be wrapped with `%w`)
- Validate() methods must not mutate receivers (set defaults in caller before validation)
- Parse user-provided times as UTC for consistent timezone handling
- Critical Stellar account fields (yes/no codes, liquidity) must error on decode failure, not log and continue

## Testing

- Test files use table-driven tests with `tests := []struct{...}`
- Run single package: `go test ./internal/model/...`
- Validation tests should cover: valid input, boundary values, empty/whitespace, malformed data
