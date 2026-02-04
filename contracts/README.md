# Soroban Contracts Deployment Guide

This guide covers deploying LMSR prediction market contracts to Stellar testnet and mainnet.

## Prerequisites

### Install Rust

```bash
# Install Rust via rustup
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

# Add WASM target
rustup target add wasm32-unknown-unknown

# Set stable as default
rustup default stable
```

### Install Stellar CLI

```bash
cargo install --locked stellar-cli
```

Verify installation:

```bash
stellar --version
# stellar 25.1.0
```

## Building Contracts

From the `contracts/` directory:

```bash
# Build both contracts
cargo build --release --target wasm32-unknown-unknown

# Output files:
# - target/wasm32-unknown-unknown/release/lmsr_market.wasm
# - target/wasm32-unknown-unknown/release/market_factory.wasm
```

Run tests before deployment:

```bash
cargo test
```

## Network Configuration

### Testnet (built-in)

Testnet is pre-configured in Stellar CLI:

```bash
# Verify testnet is available
stellar network ls
```

### Mainnet (manual setup)

Add mainnet configuration (requires your own RPC endpoint):

```bash
stellar network add mainnet \
  --rpc-url "https://your-mainnet-rpc.example.com" \
  --network-passphrase "Public Global Stellar Network ; September 2015" \
  --global
```

**Note:** Stellar does not provide public mainnet RPC nodes. Options:
- Run your own [Soroban RPC node](https://developers.stellar.org/docs/data/rpc/admin-guide)
- Use commercial providers (Validation Cloud, Ankr, etc.)

## Key Generation

### Testnet

Generate a funded testnet keypair:

```bash
stellar keys generate oracle --network testnet --fund
# Output: Key saved with alias oracle
# Output: Account funded on "Test SDF Network ; September 2015"
```

Verify the address:

```bash
stellar keys address oracle
# Output: GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU
```

### Mainnet

For mainnet, fund an existing account:

```bash
# Import existing secret key
stellar keys add oracle --secret-key
# Enter your secret key when prompted

# Or generate new (fund separately via exchange/other account)
stellar keys generate oracle --no-fund
```

## Collateral Token (SAC)

The contract supports any Stellar Asset Contract as collateral.

### Get Native XLM SAC Address

```bash
stellar contract id asset --network testnet --asset native
# Output: CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC
```

### Deploy Custom Asset SAC

```bash
stellar contract asset deploy \
  --source oracle \
  --network testnet \
  --asset EURMTL:GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V
```

## Deploying LMSR Market Contract

### Deploy

```bash
stellar contract deploy \
  --wasm target/wasm32-unknown-unknown/release/lmsr_market.wasm \
  --source oracle \
  --network testnet
# Output: CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG
```

### Initialize

```bash
stellar contract invoke \
  --id CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG \
  --source oracle \
  --network testnet \
  -- \
  initialize \
  --oracle GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU \
  --collateral_token CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC \
  --liquidity_param 1000000000 \
  --metadata_hash "QmTestMarket123" \
  --initial_funding 700000000
# Output: transfer event - 700000000 stroops (70 XLM) transferred
```

Parameters:
- `liquidity_param`: LMSR `b` parameter (100 * 10^7 = 1000000000)
- `initial_funding`: Must be >= `b * ln(2)` (~69.3 * 10^7, use 70 * 10^7 = 700000000)
- `collateral_token`: SAC contract address (XLM or any Stellar asset)

## Verified CLI Examples (Testnet)

The following examples were tested on Stellar Testnet with contract `CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG`.

### Get Price

```bash
stellar contract invoke \
  --id CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG \
  --source oracle \
  --network testnet \
  -- \
  get_price \
  --outcome 0
# Output: "5000000"
# Interpretation: 5000000 / 10^7 = 0.5 (50% probability)
```

### Get Market State

```bash
stellar contract invoke \
  --id CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG \
  --source oracle \
  --network testnet \
  -- \
  get_state
# Output: ["0","0","700000000",false]
# Interpretation: [yes_sold, no_sold, pool, resolved]
# - yes_sold: 0 tokens
# - no_sold: 0 tokens
# - pool: 700000000 stroops (70 XLM)
# - resolved: false
```

### Get Buy Quote

```bash
stellar contract invoke \
  --id CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG \
  --source oracle \
  --network testnet \
  -- \
  get_quote \
  --outcome 0 \
  --amount 100000000
# Output: ["50474900","5249791"]
# Interpretation: [cost, price_after]
# - cost: 50474900 stroops (~5.05 XLM) to buy 10 YES tokens
# - price_after: 5249791 / 10^7 = 0.525 (52.5% after purchase)
```

### Buy Tokens

```bash
stellar contract invoke \
  --id CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG \
  --source oracle \
  --network testnet \
  -- \
  buy \
  --user GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU \
  --outcome 0 \
  --amount 100000000 \
  --max_cost 55000000
# Output: "50474900"
# Event: transfer 50474900 stroops from user to contract
# Result: User received 10 YES tokens, paid ~5.05 XLM
```

### Get User Balance

```bash
stellar contract invoke \
  --id CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG \
  --source oracle \
  --network testnet \
  -- \
  get_balance \
  --user GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU \
  --outcome 0
# Output: "100000000"
# Interpretation: User has 10 YES tokens (100000000 / 10^7)
```

### Get Sell Quote

```bash
stellar contract invoke \
  --id CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG \
  --source oracle \
  --network testnet \
  -- \
  get_sell_quote \
  --outcome 0 \
  --amount 50000000
# Output: ["28726400","5124973"]
# Interpretation: [return_amount, price_after]
# - return_amount: 28726400 stroops (~2.87 XLM) for selling 5 YES tokens
# - price_after: 5124973 / 10^7 = 0.512 (51.2% after sale)
```

### Sell Tokens

```bash
stellar contract invoke \
  --id CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG \
  --source oracle \
  --network testnet \
  -- \
  sell \
  --user GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU \
  --outcome 0 \
  --amount 50000000 \
  --min_return 27000000
# Output: "28726400"
# Event: transfer 28726400 stroops from contract to user
# Result: User sold 5 YES tokens, received ~2.87 XLM
```

### Resolve Market

```bash
stellar contract invoke \
  --id CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG \
  --source oracle \
  --network testnet \
  -- \
  resolve \
  --oracle GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU \
  --winning_outcome 0
# Output: null
# Result: Market resolved, YES wins (outcome 0)
```

### Claim Winnings

```bash
stellar contract invoke \
  --id CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG \
  --source oracle \
  --network testnet \
  -- \
  claim \
  --user GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU
# Output: "49000000"
# Event: transfer 49000000 stroops from contract to user
# Result: User claimed 4.9 XLM (5 XLM - 2% fee) for 5 winning YES tokens
# Note: 2% claim fee (0.1 XLM) stays in pool for oracle to withdraw
```

### Withdraw Remaining Pool (Oracle Only)

```bash
stellar contract invoke \
  --id CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG \
  --source oracle \
  --network testnet \
  -- \
  withdraw_remaining \
  --oracle GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU
# Output: "671748500"
# Event: transfer remaining pool from contract to oracle
# Result: Oracle recovered ~67.17 XLM (leftover from losers + initial funding)
```

### Final State After All Operations

```bash
stellar contract invoke \
  --id CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG \
  --source oracle \
  --network testnet \
  -- \
  get_state
# Output: ["50000000","0","671748500",true]
# Interpretation:
# - yes_sold: 50000000 (5 tokens were sold total)
# - no_sold: 0
# - pool: 671748500 stroops (~67.17 XLM remaining)
# - resolved: true
```

## Deploying Market Factory

### Step 1: Upload LMSR Market WASM

```bash
stellar contract upload \
  --wasm target/wasm32-unknown-unknown/release/lmsr_market.wasm \
  --source oracle \
  --network testnet
# Output: 0db81ac7975fbdda91f6b68c6a4fa1845f62be3346b211866ad286eeb9f7d8dd
```

### Step 2: Deploy Factory Contract

```bash
stellar contract deploy \
  --wasm target/wasm32-unknown-unknown/release/market_factory.wasm \
  --source oracle \
  --network testnet
# Output: <FACTORY_CONTRACT_ID>
```

### Step 3: Initialize Factory

```bash
stellar contract invoke \
  --id <FACTORY_CONTRACT_ID> \
  --source oracle \
  --network testnet \
  -- \
  initialize \
  --admin GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU \
  --market_wasm_hash 0db81ac7975fbdda91f6b68c6a4fa1845f62be3346b211866ad286eeb9f7d8dd \
  --default_collateral_token CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC
```

### Deploy Market via Factory

```bash
stellar contract invoke \
  --id <FACTORY_CONTRACT_ID> \
  --source oracle \
  --network testnet \
  -- \
  deploy_market \
  --oracle GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU \
  --liquidity_param 1000000000 \
  --metadata_hash "QmYourIPFSHash" \
  --initial_funding 700000000 \
  --salt 0000000000000000000000000000000000000000000000000000000000000001
```

## Contract Functions Reference

### LMSRMarket Functions

| Function | Description | Authorization | Returns |
|----------|-------------|---------------|---------|
| `initialize` | Set up market | Oracle | `()` |
| `buy` | Buy outcome tokens | User | `i128` (actual cost) |
| `sell` | Sell outcome tokens | User | `i128` (actual return) |
| `resolve` | Resolve market | Oracle | `()` |
| `claim` | Claim winnings (2% fee deducted) | User | `i128` (amount after fee) |
| `get_price` | Get current price | None | `i128` (scaled 0-10^7) |
| `get_quote` | Get buy quote | None | `(i128, i128)` (cost, price_after) |
| `get_sell_quote` | Get sell quote | None | `(i128, i128)` (return, price_after) |
| `get_balance` | Get user balance | None | `i128` |
| `get_state` | Get market state | None | `(i128, i128, i128, bool)` |
| `withdraw_remaining` | Withdraw leftover pool after resolution | Oracle | `i128` |

### MarketFactory Functions

| Function | Description | Authorization | Returns |
|----------|-------------|---------------|---------|
| `initialize` | Set up factory | Admin | `()` |
| `deploy_market` | Deploy new market | Oracle | `Address` |
| `list_markets` | Get all markets | None | `Vec<Address>` |
| `market_count` | Get market count | None | `u32` |
| `get_market` | Get market by index | None | `Address` |
| `set_market_wasm_hash` | Update WASM hash | Admin | `()` |

## Calling Contracts from Go

### Setup

```go
import (
    "github.com/mtlprog/total/internal/soroban"
    "github.com/mtlprog/total/internal/stellar"
)

// Create clients
sorobanClient := soroban.NewClient("https://soroban-testnet.stellar.org")
stellarClient := stellar.NewHorizonClient("https://horizon-testnet.stellar.org")

// Create transaction builder
txBuilder := stellar.NewBuilder(
    stellarClient,
    "Test SDF Network ; September 2015",
    100,
    sorobanClient,
)
```

### Buy Tokens

```go
// Build buy transaction
result, err := txBuilder.BuildBuyTx(ctx, stellar.BuyTxParams{
    UserPublicKey: "GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU",
    ContractID:    "CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG",
    Outcome:       0,              // 0=YES, 1=NO
    Amount:        100_000_000,    // 10 tokens (scaled by 10^7)
    MaxCost:       55_000_000,     // Max 5.5 XLM (with slippage)
})
if err != nil {
    return err
}

// Simulate and prepare
preparedXDR, err := txBuilder.SimulateAndPrepareTx(ctx, result)
if err != nil {
    return err
}

// User signs preparedXDR externally
// Submit via sorobanClient.SendTransaction()
```

### Sell Tokens

```go
result, err := txBuilder.BuildSellTx(ctx, stellar.SellTxParams{
    UserPublicKey: "GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU",
    ContractID:    "CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG",
    Outcome:       0,              // 0=YES, 1=NO
    Amount:        50_000_000,     // 5 tokens
    MinReturn:     27_000_000,     // Min 2.7 XLM return
})
```

### Get Quote (Simulation Only)

```go
// Build quote transaction
quoteTx, err := txBuilder.BuildGetQuoteTx(ctx, stellar.GetQuoteTxParams{
    UserPublicKey: oraclePublicKey,
    ContractID:    "CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG",
    Outcome:       0,
    Amount:        100_000_000,
})

// Simulate (don't submit)
simResult, err := sorobanClient.SimulateTransaction(ctx, quoteTx)
if err != nil {
    return err
}

// Parse result tuple: (cost, price_after)
returnVal, _ := soroban.ParseReturnValue(simResult.Results[0].XDR)
tuple, _ := soroban.DecodeVec(returnVal)
cost, _ := soroban.DecodeI128(tuple[0])       // Cost in scaled units
priceAfter, _ := soroban.DecodeI128(tuple[1]) // New price (0-10^7)
```

### Resolve Market

```go
result, err := txBuilder.BuildResolveTx(ctx, stellar.ResolveTxParams{
    OraclePublicKey: "GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU",
    ContractID:      "CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG",
    WinningOutcome:  0, // 0=YES won, 1=NO won
})
```

### Claim Winnings

```go
result, err := txBuilder.BuildClaimTx(ctx, stellar.ClaimTxParams{
    UserPublicKey: "GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU",
    ContractID:    "CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG",
})
```

### Transaction Submission

```go
// Submit signed transaction
sendResult, err := sorobanClient.SendTransaction(ctx, signedXDR)
if err != nil {
    return err
}

// Wait for confirmation
txResult, err := sorobanClient.WaitForTransaction(ctx, sendResult.Hash, 30*time.Second)
if err != nil {
    return err
}

// Check result
if txResult.Status == soroban.TxResultSuccess {
    // Parse return value from txResult.ResultXdr
}
```

## Error Codes

### LMSRMarket Errors

| Code | Name | Description |
|------|------|-------------|
| 2 | NotInitialized | Market not initialized |
| 3 | AlreadyResolved | Market already resolved |
| 4 | NotResolved | Market not yet resolved |
| 5 | InvalidOutcome | Outcome must be 0 (YES) or 1 (NO) |
| 6 | InvalidAmount | Amount must be positive or funding insufficient |
| 7 | InsufficientBalance | User doesn't have enough tokens |
| 8 | SlippageExceeded | Cost exceeds max_cost |
| 9 | ReturnTooLow | Return below min_return |
| 10 | Unauthorized | Caller not authorized |
| 11 | InvalidLiquidity | Liquidity parameter invalid |
| 12 | Overflow | Arithmetic overflow |
| 13 | NothingToClaim | No winning tokens to claim |
| 14 | StorageCorrupted | Internal storage error |
| 15 | InsufficientPool | Pool has insufficient collateral |

### MarketFactory Errors

| Code | Name | Description |
|------|------|-------------|
| 1 | AlreadyInitialized | Factory already initialized |
| 2 | NotInitialized | Factory not initialized |
| 3 | Unauthorized | Only admin can do this |
| 4 | DeploymentFailed | Market deployment failed |
| 5 | IndexOutOfBounds | Market index out of bounds |
| 6 | StorageCorrupted | Internal storage error |

## Scaling

All monetary values use fixed-point arithmetic with `SCALE_FACTOR = 10^7`:

| Human Value | Scaled Value |
|-------------|--------------|
| 1 XLM | 10,000,000 |
| 0.5 (50%) | 5,000,000 |
| 100 tokens | 1,000,000,000 |

## Testnet Deployment Summary

| Item | Value |
|------|-------|
| Contract | `CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG` |
| Oracle | `GAMENVJIEUAD7U6ZB5BTPE4ENCWHZJRT3NJDS2S3JSIJGY76UGMUSGEU` |
| Collateral (XLM SAC) | `CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC` |
| Liquidity (b) | 100 XLM |
| Initial Funding | 70 XLM |
| Explorer | [stellar.expert](https://stellar.expert/explorer/testnet/contract/CBQ6R2U6P3VAADKHVI7VT26OYBMNVX3CHXU6ADI2GSKXTJXOG5I762TG) |

## Mainnet Deployment Checklist

1. **Audit contracts** - Review all contract code
2. **Test on testnet** - Full integration testing
3. **Secure keys** - Use hardware wallet for oracle/admin
4. **Set up RPC** - Configure mainnet RPC endpoint
5. **Fund accounts** - Ensure sufficient XLM for fees
6. **Deploy SAC** - Deploy collateral token SAC
7. **Deploy factory** - Deploy and initialize factory
8. **Monitor** - Set up monitoring and alerting

## References

- [Stellar CLI Documentation](https://developers.stellar.org/docs/tools/cli)
- [Soroban Smart Contracts](https://developers.stellar.org/docs/build/smart-contracts)
- [Deploy to Testnet Guide](https://developers.stellar.org/docs/build/smart-contracts/getting-started/deploy-to-testnet)
- [Stellar Asset Contract](https://developers.stellar.org/docs/tools/cli/cookbook/deploy-stellar-asset-contract)
- [go-stellar-sdk](https://github.com/stellar/go-stellar-sdk)
