use soroban_sdk::contracterror;

#[contracterror]
#[derive(Copy, Clone, Debug, Eq, PartialEq, PartialOrd, Ord)]
#[repr(u32)]
pub enum MarketError {
    /// Contract already initialized
    AlreadyInitialized = 1,
    /// Contract not initialized
    NotInitialized = 2,
    /// Market already resolved
    AlreadyResolved = 3,
    /// Market not resolved yet
    NotResolved = 4,
    /// Invalid outcome (must be 0 for YES or 1 for NO)
    InvalidOutcome = 5,
    /// Amount must be positive
    InvalidAmount = 6,
    /// Insufficient balance to sell
    InsufficientBalance = 7,
    /// Slippage exceeded - cost exceeds max_cost
    SlippageExceeded = 8,
    /// Slippage exceeded - return below min_return
    ReturnTooLow = 9,
    /// Only oracle can perform this action
    Unauthorized = 10,
    /// Liquidity parameter must be positive
    InvalidLiquidity = 11,
    /// Arithmetic overflow
    Overflow = 12,
    /// User has no winning tokens to claim
    NothingToClaim = 13,
    /// Critical storage data missing (contract state corrupted)
    StorageCorrupted = 14,
}
