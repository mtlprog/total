# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Total

Stellar prediction market platform. Stateless web application that builds Soroban contract invocation transactions for users to sign externally.

## Commands

- `make build` - Build for local macOS
- `make run` - Run locally (requires ORACLE_PUBLIC_KEY env var)
- `make test` - Run tests
- `make lint` - Format and vet code
- `cd contracts && cargo test` - Run Soroban contract tests
- `cd contracts && cargo build --release --target wasm32-unknown-unknown` - Build Soroban WASM
- `rustup default stable` - Required before cargo commands on fresh Rust install

## Project Structure

```
cmd/total/         - CLI entry point
internal/
├── chart/         - ASCII price charts
├── config/        - Configuration constants (Stellar, Soroban)
├── handler/       - HTTP request handlers
├── ipfs/          - Pinata IPFS client (unused, legacy)
├── lmsr/          - LMSR pricing calculator (Go)
├── logger/        - Structured logging (slog/JSON)
├── model/         - Data structures (Market, Quote, etc.)
├── service/       - Business logic (MarketService)
├── soroban/       - Soroban RPC client and helpers
├── stellar/       - Stellar client and transaction builder
└── template/      - HTML templates
contracts/
├── lmsr_market/   - LMSR market Soroban contract (Rust)
│   └── src/
│       ├── lib.rs     - Main contract
│       ├── lmsr.rs    - LMSR math (fixed-point)
│       ├── storage.rs - Storage keys
│       └── error.rs   - Contract errors
└── market_factory/ - Factory contract for deploying markets
```

## Stack

- Go 1.24+
- github.com/stellar/go-stellar-sdk (Horizon client, txnbuild)
- LMSR (Logarithmic Market Scoring Rule) for pricing
- No database - all state from Soroban contracts
- Rust + Soroban SDK for smart contracts

## Architecture

### Soroban Architecture
- Markets are Soroban smart contracts
- Users invoke buy() directly - no oracle signature per trade
- LMSR pricing calculated atomically in contract
- State stored in contract instance storage

### LMSR Pricing
- Price formula: `P(yes) = e^(qYes/b) / (e^(qYes/b) + e^(qNo/b))`
- Cost function: `C(q) = b * ln(e^(qYes/b) + e^(qNo/b))`
- Parameter `b` controls liquidity depth
- Initial funding = `b * ln(2)` EURMTL
- LMSR is symmetric: buying and immediately selling same amount returns same cost (no spread)
- Use `get_sell_quote` for sell transactions, not `get_quote` (they return different values)

### Market Lifecycle
1. Oracle deploys market contract via factory
2. Users buy outcome tokens (EURMTL → YES/NO tokens)
3. Oracle resolves market
4. Winners claim EURMTL via contract

## Environment Variables

- `ORACLE_PUBLIC_KEY` (required) - Stellar account that creates/resolves markets
- `SOROBAN_RPC_URL` (required) - Soroban RPC endpoint
- `HORIZON_URL` - Horizon API URL for account lookups (default: mainnet)
- `NETWORK_PASSPHRASE` - Stellar network passphrase
- `CONTRACT_IDS` - Comma-separated list of known market contract IDs (C...)
- `MARKET_FACTORY_CONTRACT` - Factory contract ID (C...)
- `PORT` - HTTP server port (default: 8080)
- `LOG_LEVEL` - Log level (debug, info, warn, error)

## Gotchas

### General
- `.gitignore`: use `/total` not `total` to avoid ignoring `cmd/total/`
- Stellar SDK moved from `stellar/go` to `stellar/go-stellar-sdk` (Dec 2025)
- Use `errors.Is()` not `==` for error comparison (errors may be wrapped with `%w`)
- Validate() methods must not mutate receivers (set defaults in caller before validation)
- Parse user-provided times as UTC for consistent timezone handling
- Critical Stellar account fields (yes/no codes, liquidity) must error on decode failure, not log and continue

### Soroban
- All amounts use fixed-point with SCALE_FACTOR = 10^7 (matches Stellar precision)
- Contract addresses start with 'C', account addresses start with 'G'
- InvokeHostFunction transactions require simulation before submission
- Soroban transactions need resources (CPU, memory) attached from simulation
- Auth entries from simulation must be included in final transaction
- ContractId is typedef of Hash, not a pointer - use `var id xdr.ContractId`
- LMSR math uses Taylor series for exp/ln - handle overflow carefully
- Contract storage uses instance storage for all market state
- Tokens are internal balances (no Stellar trustlines needed in Soroban mode)

### Soroban Contract Development
- Use `#![no_std]` - standard library not available
- All arithmetic must handle overflow (use checked_* methods)
- Test with `soroban-sdk` testutils feature
- Deploy with `soroban contract deploy` CLI
- Initialize SAC with `stellar contract asset deploy`
- In tests, `#[should_panic(expected = "...")]` must use error codes like `"Error(Contract, #7)"`, not error names
- Avoid `.unwrap()` on storage access - use `.ok_or(MarketError::StorageCorrupted)?` for proper error handling
- Always guard pool subtraction: `if pool < amount { return Err(MarketError::InsufficientPool); }`
- Document token_client.transfer() panics with comments (they can fail on insufficient balance)
- Error codes: NotInitialized=#2, AlreadyResolved=#3, NotResolved=#4, InvalidOutcome=#5, InvalidAmount=#6, InsufficientBalance=#7, SlippageExceeded=#8, ReturnTooLow=#9, Unauthorized=#10, InvalidLiquidity=#11, Overflow=#12, NothingToClaim=#13, StorageCorrupted=#14, InsufficientPool=#15

### Refactoring Patterns
- When moving types between packages (e.g., model → service), update tests that reference them
- After removing code, check for unused imports (`go build` will catch these)
- Request types (BuyRequest, SellRequest, etc.) live in service package, not model

## Testing

- Test files use table-driven tests with `tests := []struct{...}`
- Run single package: `go test ./internal/model/...`
- Validation tests should cover: valid input, boundary values, empty/whitespace, malformed data
- Soroban contracts: `cd contracts && cargo test`
- LMSR math tests verify exp/ln accuracy and price calculations

## Git Conventions

Follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

Types:
- `feat` - New feature
- `fix` - Bug fix
- `docs` - Documentation only
- `refactor` - Code change that neither fixes a bug nor adds a feature
- `test` - Adding or updating tests
- `chore` - Maintenance tasks (deps, CI, etc.)

Examples:
- `feat: add LMSR prediction market`
- `fix: handle pool underflow in sell()`
- `docs: add Soroban error codes to CLAUDE.md`
- `refactor: migrate to Soroban-only architecture`
- `test: add Market.Validate() tests`

## Soroban Contract Functions

### LMSRMarket Contract
- `initialize(oracle, collateral_token, liquidity_param, metadata_hash, initial_funding)` - Set up market
- `buy(user, outcome, amount, max_cost)` - Buy tokens, returns actual cost
- `sell(user, outcome, amount, min_return)` - Sell tokens, returns actual return
- `resolve(oracle, winning_outcome)` - Oracle resolves market
- `claim(user)` - Claim winnings after resolution
- `get_price(outcome)` - Get current price (0-SCALE_FACTOR)
- `get_quote(outcome, amount)` - Get buy quote (cost, price_after)
- `get_sell_quote(outcome, amount)` - Get sell quote (return_amount, price_after)
- `get_balance(user, outcome)` - Get user's token balance
- `get_state()` - Get (yes_sold, no_sold, pool, resolved)

### MarketFactory Contract
- `initialize(admin, market_wasm_hash, default_collateral_token)` - Set up factory
- `deploy_market(oracle, liquidity_param, metadata_hash, initial_funding, salt)` - Deploy new market
- `list_markets()` - Get all deployed market addresses
- `market_count()` - Get number of markets
