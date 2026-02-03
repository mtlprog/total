# Total - Stellar Prediction Markets

A prediction market platform built on the Stellar blockchain. Users bet on binary (YES/NO) outcomes using EURMTL tokens, with prices determined by the Logarithmic Market Scoring Rule (LMSR).

## Architecture Options

Total supports two operating modes:

### Classic Mode (Stellar Accounts)
- Markets are Stellar accounts with multisig
- Oracle must sign every trade transaction
- Simpler setup, but centralized trade authorization

### Soroban Mode (Smart Contracts)
- Markets are Soroban smart contracts
- Users invoke `buy()` directly - **no oracle signature per trade**
- LMSR pricing calculated atomically in contract
- Trustless, autonomous operation

```
┌─────────────────────────────────────────────────────────────┐
│                    Go Web Application                        │
│  - Classic: Builds multi-op transactions                    │
│  - Soroban: Builds InvokeHostFunction transactions          │
│  - Reads state via Horizon API / Soroban RPC                │
└─────────────────────────────────────────────────────────────┘
                              │
            ┌─────────────────┴─────────────────┐
            │                                   │
            ▼                                   ▼
┌───────────────────────┐           ┌───────────────────────┐
│   Classic Mode        │           │   Soroban Mode        │
│   (Stellar Accounts)  │           │   (Smart Contracts)   │
│                       │           │                       │
│  - Market = Account   │           │  MarketFactory        │
│  - Oracle multisig    │           │  - deploy_market()    │
│  - ManageData entries │           │  - list_markets()     │
│                       │           │                       │
│                       │           │  LMSRMarket           │
│                       │           │  - buy() / sell()     │
│                       │           │  - resolve() [oracle] │
│                       │           │  - claim()            │
└───────────────────────┘           └───────────────────────┘
```

## Quick Start

### Classic Mode

```bash
# Build the application
make build

# Run with your oracle public key
ORACLE_PUBLIC_KEY=G... make run

# Or run directly
./total serve --oracle-public-key "GABCD..."
```

### Soroban Mode

```bash
# Deploy contracts first (see Soroban Deployment section)

# Run with Soroban enabled
./total serve \
  --oracle-public-key "GABCD..." \
  --use-soroban \
  --soroban-rpc-url "https://soroban-testnet.stellar.org:443" \
  --market-factory-contract "CDEF..." \
  --eurmtl-contract "CGHI..."
```

The web interface will be available at http://localhost:8080.

## Prerequisites

### Stellar Account

You need a Stellar account to interact with markets. If you don't have one:

1. Go to [Stellar Laboratory](https://laboratory.stellar.org/#account-creator?network=public)
2. Click "Generate Keypair" to create a new account
3. **Save your Secret Key (S...) securely** - you'll need it to sign transactions
4. Fund your account with XLM and establish a trustline to EURMTL

### EURMTL Token

All bets use EURMTL as collateral:
- **Asset Code:** `EURMTL`
- **Issuer:** `GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V`

To get EURMTL:
1. Establish a trustline to EURMTL using [Stellar Laboratory](https://laboratory.stellar.org)
2. Acquire EURMTL through the Stellar DEX or MTL community

## How Prediction Markets Work

### The Concept

A prediction market allows you to bet on the outcome of future events. Each market has a binary question with YES or NO outcomes. The price of each token (0% to 100%) reflects the market's collective belief in that outcome's probability.

### Token Types

Each market issues two types of tokens:
- **YES tokens** - worth 1 EURMTL if the outcome is YES, 0 otherwise
- **NO tokens** - worth 1 EURMTL if the outcome is NO, 0 otherwise

### LMSR Pricing

Prices are calculated using the [Logarithmic Market Scoring Rule](https://gnosis-pm-js.readthedocs.io/en/v1.3.0/lmsr-primer.html):

```
P(YES) = e^(qYes/b) / (e^(qYes/b) + e^(qNo/b))
```

Where:
- `qYes`, `qNo` = total tokens sold for each outcome
- `b` = liquidity parameter (higher = lower price impact)
- Prices always sum to 100%

The cost to buy tokens depends on how much your purchase shifts the market:
```
Cost = b * ln(e^(newQYes/b) + e^(newQNo/b)) - b * ln(e^(oldQYes/b) + e^(oldQNo/b))
```

### Market Lifecycle

```
CREATE          TRADE           RESOLVE         REDEEM
  |               |               |               |
  v               v               v               v
Oracle funds -> Users buy  -> Oracle sets -> Winners get
the market      YES/NO         winner        1 EURMTL per
                tokens                       token
```

## User Guide

### Viewing Markets

Visit the homepage (http://localhost:8080) to see all active markets. Each market shows:
- The prediction question
- Current YES/NO prices (probability)
- Trading volume

Click on a market to see details and place bets.

### Buying Tokens (Placing a Bet)

1. **Navigate** to a market's detail page
2. **Enter your Stellar public key** (G...)
3. **Select outcome** - YES or NO
4. **Enter amount** - number of tokens to buy
5. **Click "Get Transaction XDR"**

The application generates an unsigned XDR transaction. You must sign and submit it:

1. Copy the XDR blob
2. Go to [Stellar Laboratory Transaction Signer](https://laboratory.stellar.org/#txsigner?network=public)
3. Paste the XDR
4. Sign with your secret key
5. Submit the signed transaction

**What happens on-chain:**
- You pay EURMTL to the market account
- You receive YES or NO tokens from the market
- The market's price updates based on LMSR

### Getting a Quote

Before buying, you can get a price quote:
1. Enter the outcome and amount on the market page
2. The quote shows:
   - **Total cost** in EURMTL
   - **Price per share** (average price)
   - **New probability** after your purchase

Larger purchases have higher price impact due to LMSR.

### After Resolution

When the oracle resolves a market:
- **Winning tokens** are redeemable for 1 EURMTL each
- **Losing tokens** are worth nothing

To claim winnings:
1. Send your winning tokens back to the market account
2. Receive EURMTL in return (1:1 ratio)

## Oracle Guide

The oracle is the trusted account that creates and resolves markets.

### Creating a Market

1. Go to `/create` page
2. Fill in:
   - **Question** - The prediction question (clear YES/NO answer)
   - **Description** - Resolution criteria (be specific!)
   - **Close Time** - When trading ends
   - **Liquidity Parameter (b)** - Higher = more liquidity, higher max loss

3. Click "Generate Create Transaction"
4. Sign and submit with the oracle secret key

**Initial funding required:** `b * ln(2)` EURMTL

For example:
- b=100 -> 69.31 EURMTL initial funding
- b=1000 -> 693.15 EURMTL initial funding

### Resolving a Market

When the outcome is known:

1. Navigate to the market detail page
2. Find the "Resolve Market" section (oracle only)
3. Select the winning outcome (YES or NO)
4. Generate, sign, and submit the resolve transaction

After resolution:
- The `resolution` data entry is set on the market account
- Winners can redeem tokens for EURMTL

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ORACLE_PUBLIC_KEY` | Yes | - | Stellar account that creates/resolves markets |
| `ORACLE_SECRET_KEY` | No | - | Secret key for signing (optional, for server-side signing) |
| `PORT` | No | 8080 | HTTP server port |
| `HORIZON_URL` | No | mainnet | Stellar Horizon API URL |
| `NETWORK_PASSPHRASE` | No | mainnet | Stellar network passphrase |
| `PINATA_API_KEY` | No | - | Pinata API key for IPFS metadata |
| `PINATA_SECRET` | No | - | Pinata API secret |
| `MARKET_IDS` | No | - | Comma-separated list of known market IDs |
| `LOG_LEVEL` | No | info | Log level (debug, info, warn, error) |
| `USE_SOROBAN` | No | false | Enable Soroban smart contract mode |
| `SOROBAN_RPC_URL` | No | mainnet | Soroban RPC URL |
| `MARKET_FACTORY_CONTRACT` | No | - | Market factory contract ID (Soroban mode) |
| `EURMTL_CONTRACT` | No | - | EURMTL Stellar Asset Contract ID (Soroban mode) |

### Command Line Flags

```bash
# Classic mode
./total serve \
  --oracle-public-key "GABCD..." \
  --port 8080 \
  --horizon-url "https://horizon.stellar.org" \
  --network-passphrase "Public Global Stellar Network ; September 2015" \
  --pinata-api-key "..." \
  --pinata-secret "..." \
  --market-ids "GA123...,GB456..."

# Soroban mode
./total serve \
  --oracle-public-key "GABCD..." \
  --use-soroban \
  --soroban-rpc-url "https://soroban-testnet.stellar.org:443" \
  --market-factory-contract "CDEF..." \
  --eurmtl-contract "CGHI..."
```

## Soroban Deployment

### Prerequisites

```bash
# Install Rust
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
source ~/.cargo/env

# Add WASM target
rustup target add wasm32-unknown-unknown

# Install Soroban CLI
cargo install --locked soroban-cli

# Verify
soroban --version
```

### Configure Network

```bash
# Add testnet
soroban network add --global testnet \
  --rpc-url https://soroban-testnet.stellar.org:443 \
  --network-passphrase "Test SDF Network ; September 2015"

# Generate deployer identity
soroban keys generate --global testnet-deployer --network testnet
soroban keys address testnet-deployer

# Fund via friendbot
curl "https://friendbot.stellar.org/?addr=$(soroban keys address testnet-deployer)"
```

### Build Contracts

```bash
cd contracts

# Build both contracts
cargo build --release --target wasm32-unknown-unknown

# The WASM files will be at:
# target/wasm32-unknown-unknown/release/lmsr_market.wasm
# target/wasm32-unknown-unknown/release/market_factory.wasm
```

### Deploy Contracts

```bash
# Deploy LMSR market contract and get WASM hash
soroban contract deploy \
  --wasm target/wasm32-unknown-unknown/release/lmsr_market.wasm \
  --source testnet-deployer \
  --network testnet

# Deploy market factory with the WASM hash
soroban contract deploy \
  --wasm target/wasm32-unknown-unknown/release/market_factory.wasm \
  --source testnet-deployer \
  --network testnet

# Deploy EURMTL SAC (Stellar Asset Contract)
stellar contract asset deploy \
  --asset EURMTL:GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V \
  --source testnet-deployer \
  --network testnet
```

### Initialize Factory

```bash
soroban contract invoke \
  --id <FACTORY_CONTRACT_ID> \
  --source testnet-deployer \
  --network testnet \
  -- initialize \
  --admin <ORACLE_PUBLIC_KEY> \
  --market_wasm_hash <LMSR_MARKET_WASM_HASH> \
  --default_collateral_token <EURMTL_SAC_ID>
```

## Development

```bash
# Run tests
make test

# Format and lint
make lint

# Build for Docker/Linux
make build-linux

# Test Soroban contracts
cd contracts && cargo test
```

## Example Workflow

### 1. Oracle Creates Market

```
Question: "Will BTC reach $100k by December 2026?"
Description: "Resolves YES if Bitcoin price on Coinbase reaches $100,000 USD
             at any point before December 31, 2026 23:59 UTC."
Liquidity: b=500
Initial funding: 346.57 EURMTL
```

### 2. User Buys YES Tokens

```
Current price: YES=50%, NO=50%
User buys: 100 YES tokens
Cost: ~57.79 EURMTL (price impact from 50% to 55%)
New price: YES=55%, NO=45%
```

**In Soroban mode:** User signs only their own transaction - no oracle needed!

### 3. More Trading

As more users buy YES tokens, the price increases:
- YES price of 70% means the market believes there's a 70% chance of YES
- Traders profit by buying when they believe the market is wrong

### 4. Resolution

Oracle determines BTC did reach $100k:
```
Resolution: YES wins
```

### 5. Redemption

- Users with YES tokens redeem for 1 EURMTL each
- Users with NO tokens get nothing
- User who bought 100 YES for 57.79 EURMTL receives 100 EURMTL (profit: 42.21 EURMTL)

## Security Considerations

- **Never share your secret key** - only sign transactions you understand
- **XDR is safe to share** - it contains no private keys
- **Verify transactions** before signing in Stellar Laboratory
- **Oracle is trusted** - it controls market resolution
- **EURMTL is the collateral** - ensure you trust the issuer
- **Slippage protection** - buy transactions include max cost limits
- **Soroban contracts are immutable** - audit before trusting

## License

MIT
