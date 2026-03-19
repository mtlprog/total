#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Production Deployment Script for MTL Predict
# =============================================================================
# Prepares all transactions for mainnet deployment.
# Transactions are output as XDR for manual signing.
#
# Prerequisites:
#   - stellar-cli installed (v25+)
#   - Rust + wasm32-unknown-unknown target
#   - contracts built: cd contracts && cargo build --release --target wasm32-unknown-unknown
#
# Usage:
#   ./scripts/deploy-prod.sh
# =============================================================================

NETWORK="mainnet"
ORACLE="GBVU4OGNKWYJXW3FWTT2WVY6OELGMHFYWPYNDBZWIIRBJWAACLORACLE"

# EURMTL SAC address on mainnet
# stellar contract id asset --network mainnet --asset EURMTL:GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V
COLLATERAL_TOKEN="CDUYP3U6HGTOBUNQD2WTLWNMNADWMENROKZZIHGEVGKIU3ZUDF42CDOK"

LMSR_WASM="contracts/target/wasm32-unknown-unknown/release/lmsr_market.wasm"
FACTORY_WASM="contracts/target/wasm32-unknown-unknown/release/market_factory.wasm"

echo "=== MTL Predict — Production Deployment ==="
echo ""
echo "Network:          $NETWORK"
echo "Oracle:           $ORACLE"
echo "Collateral token: EURMTL ($COLLATERAL_TOKEN)"
echo ""

# ---- Step 0: Verify WASM binaries exist ----
if [[ ! -f "$LMSR_WASM" ]]; then
  echo "ERROR: LMSR WASM not found at $LMSR_WASM"
  echo "Run: cd contracts && cargo build --release --target wasm32-unknown-unknown"
  exit 1
fi

if [[ ! -f "$FACTORY_WASM" ]]; then
  echo "ERROR: Factory WASM not found at $FACTORY_WASM"
  echo "Run: cd contracts && cargo build --release --target wasm32-unknown-unknown"
  exit 1
fi

echo "LMSR WASM:    $LMSR_WASM ($(wc -c < "$LMSR_WASM" | tr -d ' ') bytes)"
echo "Factory WASM: $FACTORY_WASM ($(wc -c < "$FACTORY_WASM" | tr -d ' ') bytes)"
echo ""

# ---- Step 1: Install LMSR WASM (get hash for factory) ----
echo "=== Step 1: Install LMSR Market WASM ==="
echo "This uploads the WASM bytecode to the network and returns a hash."
echo ""
echo "Run:"
echo "  stellar contract install \\"
echo "    --wasm $LMSR_WASM \\"
echo "    --source $ORACLE \\"
echo "    --network $NETWORK \\"
echo "    --build-only"
echo ""
echo "Sign and submit:"
echo "  stellar tx sign --sign-with-key <your-key> <xdr>"
echo "  stellar tx send --network $NETWORK <signed_xdr>"
echo ""
echo "Save the returned WASM hash for Step 3."
echo ""
read -rp "Enter LMSR WASM hash: " LMSR_WASM_HASH
echo ""

# ---- Step 2: Deploy Factory Contract ----
echo "=== Step 2: Deploy Factory Contract ==="
echo ""
echo "Run:"
echo "  stellar contract deploy \\"
echo "    --wasm $FACTORY_WASM \\"
echo "    --source $ORACLE \\"
echo "    --network $NETWORK \\"
echo "    --build-only"
echo ""
echo "Sign and submit. Save the returned contract ID (C...)."
echo ""
read -rp "Enter Factory contract ID: " FACTORY_CONTRACT
echo ""

# ---- Step 3: Initialize Factory ----
echo "=== Step 3: Initialize Factory ==="
echo ""
echo "Run:"
echo "  stellar contract invoke \\"
echo "    --id $FACTORY_CONTRACT \\"
echo "    --source $ORACLE \\"
echo "    --network $NETWORK \\"
echo "    --build-only \\"
echo "    -- \\"
echo "    initialize \\"
echo "    --admin $ORACLE \\"
echo "    --market_wasm_hash $LMSR_WASM_HASH \\"
echo "    --default_collateral_token $COLLATERAL_TOKEN"
echo ""
echo "Sign and submit."
echo ""

# ---- Step 4: Update .env.prod ----
echo "=== Step 4: Update .env.prod ==="
echo ""
if [[ -n "${FACTORY_CONTRACT:-}" ]]; then
  sed -i '' "s/^MARKET_FACTORY_CONTRACT=.*/MARKET_FACTORY_CONTRACT=$FACTORY_CONTRACT/" .env.prod
  echo "Updated .env.prod: MARKET_FACTORY_CONTRACT=$FACTORY_CONTRACT"
else
  echo "Manually set MARKET_FACTORY_CONTRACT in .env.prod"
fi

echo ""
echo "=== Deployment Complete ==="
echo ""
echo "To run with production config:"
echo "  cp .env.prod .env && make dev"
