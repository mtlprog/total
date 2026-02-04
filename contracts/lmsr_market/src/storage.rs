use soroban_sdk::{contracttype, Address};

/// Storage keys for the contract.
/// Using enum with variants for type-safe storage access.
#[derive(Clone)]
#[contracttype]
pub enum DataKey {
    /// Oracle address (can resolve market)
    Oracle,
    /// Collateral token contract address (e.g., EURMTL SAC)
    CollateralToken,
    /// LMSR liquidity parameter (b) scaled by SCALE_FACTOR
    LiquidityParam,
    /// Quantity of YES tokens sold (scaled)
    YesSold,
    /// Quantity of NO tokens sold (scaled)
    NoSold,
    /// Total collateral held in contract (scaled)
    CollateralPool,
    /// Whether market is resolved
    Resolved,
    /// Winning outcome (0 = YES, 1 = NO)
    WinningOutcome,
    /// IPFS metadata hash
    MetadataHash,
    /// User balance for outcome tokens: UserBalance(user, outcome)
    UserBalance(Address, u32),
}

/// Outcome constants
pub const OUTCOME_YES: u32 = 0;
pub const OUTCOME_NO: u32 = 1;

/// Scale factor for fixed-point arithmetic.
/// Uses 7 decimal places to match Stellar/Soroban native token precision,
/// ensuring seamless conversion between contract amounts and on-chain balances.
pub const SCALE_FACTOR: i128 = 10_000_000; // 10^7

/// Natural log of 2 scaled (ln(2) * SCALE_FACTOR).
/// ln(2) ≈ 0.6931472
/// Used for initial liquidity calculation: b * ln(2).
pub const LN2_SCALED: i128 = 6_931_472;

/// Claim fee in basis points (1 bp = 0.01%).
/// 200 bp = 2% fee on winnings.
/// Fee stays in pool and goes to oracle via withdraw_remaining.
pub const CLAIM_FEE_BPS: i128 = 200;

/// Basis points denominator (100% = 10000 bp).
pub const BPS_DENOMINATOR: i128 = 10_000;
