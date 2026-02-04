# LMSR Market Contracts

Soroban smart contracts for LMSR prediction markets on Stellar.

## Quick Start

```bash
# Install Rust + WASM target
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
rustup target add wasm32-unknown-unknown

# Install Stellar CLI
cargo install --locked stellar-cli

# Build contracts
cargo build --release --target wasm32-unknown-unknown

# Run tests
cargo test
```

## Deploy to Testnet

```bash
# Generate funded keypair
stellar keys generate oracle --network testnet --fund

# Get XLM SAC address
stellar contract id asset --network testnet --asset native
# CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC

# Deploy contract
stellar contract deploy \
  --wasm target/wasm32-unknown-unknown/release/lmsr_market.wasm \
  --source oracle \
  --network testnet
# Returns: <CONTRACT_ID>

# Initialize (b=100 XLM, funding=70 XLM)
stellar contract invoke \
  --id <CONTRACT_ID> \
  --source oracle \
  --network testnet \
  -- \
  initialize \
  --oracle $(stellar keys address oracle) \
  --collateral_token CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC \
  --liquidity_param 1000000000 \
  --metadata_hash "QmTest" \
  --initial_funding 700000000
```

## Market Lifecycle

All amounts scaled by 10^7 (1 XLM = 10000000).

### 1. Check Price & Quote

```bash
# Get YES price (returns 0-10000000, where 5000000 = 50%)
stellar contract invoke --id <CONTRACT_ID> --source oracle --network testnet \
  -- get_price --outcome 0

# Get quote for buying 10 YES tokens
stellar contract invoke --id <CONTRACT_ID> --source oracle --network testnet \
  -- get_quote --outcome 0 --amount 100000000
# Returns: [cost, price_after]
```

### 2. Buy Tokens

```bash
# Buy 10 YES tokens (max cost 6 XLM)
stellar contract invoke --id <CONTRACT_ID> --source user --network testnet \
  -- buy --user <USER_ADDRESS> --outcome 0 --amount 100000000 --max_cost 60000000
# Returns: actual cost
```

### 3. Sell Tokens

```bash
# Sell 5 YES tokens (min return 2.5 XLM)
stellar contract invoke --id <CONTRACT_ID> --source user --network testnet \
  -- sell --user <USER_ADDRESS> --outcome 0 --amount 50000000 --min_return 25000000
# Returns: actual return
```

### 4. Resolve Market (Oracle Only)

```bash
# Resolve: 0=YES wins, 1=NO wins
stellar contract invoke --id <CONTRACT_ID> --source oracle --network testnet \
  -- resolve --oracle <ORACLE_ADDRESS> --winning_outcome 0
```

### 5. Claim Winnings

```bash
# Winners claim (2% fee deducted)
stellar contract invoke --id <CONTRACT_ID> --source user --network testnet \
  -- claim --user <USER_ADDRESS>
# Returns: payout (tokens * 0.98)
```

### 6. Withdraw Remaining (Oracle Only)

```bash
# Oracle withdraws leftover pool (losers' funds + fees)
stellar contract invoke --id <CONTRACT_ID> --source oracle --network testnet \
  -- withdraw_remaining --oracle <ORACLE_ADDRESS>
# Returns: amount withdrawn
```

### Check State

```bash
# Get market state
stellar contract invoke --id <CONTRACT_ID> --source oracle --network testnet \
  -- get_state
# Returns: [yes_sold, no_sold, pool, resolved]

# Get user balance
stellar contract invoke --id <CONTRACT_ID> --source oracle --network testnet \
  -- get_balance --user <USER_ADDRESS> --outcome 0
```

## Functions

| Function | Args | Returns |
|----------|------|---------|
| `initialize` | oracle, collateral_token, liquidity_param, metadata_hash, initial_funding | - |
| `buy` | user, outcome, amount, max_cost | cost |
| `sell` | user, outcome, amount, min_return | return |
| `resolve` | oracle, winning_outcome | - |
| `claim` | user | payout (after 2% fee) |
| `withdraw_remaining` | oracle | amount |
| `get_price` | outcome | price (0-10^7) |
| `get_quote` | outcome, amount | (cost, price_after) |
| `get_sell_quote` | outcome, amount | (return, price_after) |
| `get_balance` | user, outcome | balance |
| `get_state` | - | (yes_sold, no_sold, pool, resolved) |

## Error Codes

| Code | Error |
|------|-------|
| 2 | NotInitialized |
| 3 | AlreadyResolved |
| 4 | NotResolved |
| 5 | InvalidOutcome |
| 6 | InvalidAmount |
| 7 | InsufficientBalance |
| 8 | SlippageExceeded |
| 9 | ReturnTooLow |
| 10 | Unauthorized |
| 11 | InvalidLiquidity |
| 12 | Overflow |
| 13 | NothingToClaim |
| 14 | StorageCorrupted |
| 15 | InsufficientPool |

## Scaling

All amounts use SCALE_FACTOR = 10^7:
- 1 XLM = 10,000,000
- 50% = 5,000,000
- initial_funding >= liquidity_param * 0.693
