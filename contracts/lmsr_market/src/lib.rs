#![no_std]

mod error;
mod lmsr;
mod storage;

use error::MarketError;
use soroban_sdk::{contract, contractimpl, token, Address, Env, String};
use storage::{DataKey, OUTCOME_NO, OUTCOME_YES, BPS_DENOMINATOR, CLAIM_FEE_BPS};
#[cfg(test)]
use storage::SCALE_FACTOR;

/// LMSR Prediction Market Contract
///
/// This contract implements a binary prediction market using the
/// Logarithmic Market Scoring Rule (LMSR) for automated market making.
///
/// Key features:
/// - Users can buy/sell YES and NO outcome tokens
/// - Prices are calculated using LMSR formula
/// - Oracle resolves the market to determine winning outcome
/// - Winners can claim their collateral (1:1 redemption)
#[contract]
pub struct LmsrMarket;

#[contractimpl]
impl LmsrMarket {
    /// Constructor: Called automatically when deployed via factory's deploy_v2.
    ///
    /// Delegates to initialize() for the actual setup logic.
    pub fn __constructor(
        env: Env,
        oracle: Address,
        collateral_token: Address,
        liquidity_param: i128,
        metadata_hash: String,
        initial_funding: i128,
    ) {
        Self::initialize(env, oracle, collateral_token, liquidity_param, metadata_hash, initial_funding)
            .expect("initialization failed");
    }

    /// Initialize the market with oracle, collateral token, liquidity parameter, and metadata.
    ///
    /// Can be called directly for manual deployment, or via constructor for factory deployment.
    ///
    /// # Arguments
    /// * `oracle` - Address that can resolve the market
    /// * `collateral_token` - Token contract for collateral (e.g., EURMTL SAC)
    /// * `liquidity_param` - LMSR b parameter (scaled by 10^7)
    /// * `metadata_hash` - IPFS hash for market metadata
    /// * `initial_funding` - Initial collateral to fund the market (scaled by 10^7)
    pub fn initialize(
        env: Env,
        oracle: Address,
        collateral_token: Address,
        liquidity_param: i128,
        metadata_hash: String,
        initial_funding: i128,
    ) -> Result<(), MarketError> {
        // Check not already initialized
        if env.storage().instance().has(&DataKey::Oracle) {
            return Err(MarketError::AlreadyInitialized);
        }

        if liquidity_param <= 0 {
            return Err(MarketError::InvalidLiquidity);
        }

        // Verify initial funding matches required liquidity
        let required = lmsr::initial_liquidity(liquidity_param)?;
        if initial_funding < required {
            return Err(MarketError::InvalidAmount);
        }

        // Verify initial funding doesn't risk overflow in pool operations
        // Using i128::MAX / 2 as a safe upper bound for funding
        const MAX_FUNDING: i128 = i128::MAX / 2;
        if initial_funding > MAX_FUNDING {
            return Err(MarketError::Overflow);
        }

        // Oracle must authorize the initialization (they provide initial funding)
        oracle.require_auth();

        // Transfer initial funding from oracle to contract
        // Note: token_client.transfer() may panic on failure (e.g., insufficient balance,
        // authorization issues). These panics are appropriate as they indicate the oracle
        // does not have sufficient funds or proper authorization.
        let token_client = token::Client::new(&env, &collateral_token);
        token_client.transfer(&oracle, &env.current_contract_address(), &initial_funding);

        // Store contract state
        env.storage().instance().set(&DataKey::Oracle, &oracle);
        env.storage()
            .instance()
            .set(&DataKey::CollateralToken, &collateral_token);
        env.storage()
            .instance()
            .set(&DataKey::LiquidityParam, &liquidity_param);
        env.storage()
            .instance()
            .set(&DataKey::MetadataHash, &metadata_hash);
        env.storage().instance().set(&DataKey::YesSold, &0i128);
        env.storage().instance().set(&DataKey::NoSold, &0i128);
        env.storage()
            .instance()
            .set(&DataKey::CollateralPool, &initial_funding);
        env.storage().instance().set(&DataKey::Resolved, &false);

        Ok(())
    }

    /// Buy outcome tokens.
    ///
    /// # Arguments
    /// * `user` - User buying tokens (must authorize)
    /// * `outcome` - 0 for YES, 1 for NO
    /// * `amount` - Amount of tokens to buy (scaled by 10^7)
    /// * `max_cost` - Maximum collateral willing to pay (slippage protection).
    ///                The transaction reverts if calculated cost exceeds this value,
    ///                protecting users from price movements between quote and execution.
    ///
    /// # Returns
    /// Actual cost paid in collateral
    pub fn buy(
        env: Env,
        user: Address,
        outcome: u32,
        amount: i128,
        max_cost: i128,
    ) -> Result<i128, MarketError> {
        Self::require_initialized(&env)?;
        Self::require_not_resolved(&env)?;

        if outcome != OUTCOME_YES && outcome != OUTCOME_NO {
            return Err(MarketError::InvalidOutcome);
        }
        if amount <= 0 {
            return Err(MarketError::InvalidAmount);
        }

        // User must authorize the buy
        user.require_auth();

        // Get current state
        let b: i128 = env
            .storage()
            .instance()
            .get(&DataKey::LiquidityParam)
            .ok_or(MarketError::StorageCorrupted)?;
        let q_yes: i128 = env
            .storage()
            .instance()
            .get(&DataKey::YesSold)
            .ok_or(MarketError::StorageCorrupted)?;
        let q_no: i128 = env
            .storage()
            .instance()
            .get(&DataKey::NoSold)
            .ok_or(MarketError::StorageCorrupted)?;

        // Calculate cost
        let cost = lmsr::calculate_buy_cost(q_yes, q_no, amount, outcome, b)?;

        if cost > max_cost {
            return Err(MarketError::SlippageExceeded);
        }

        // Transfer collateral from user to contract
        // Note: token_client.transfer() may panic on failure (e.g., insufficient balance,
        // authorization issues). These panics are appropriate as they indicate the user
        // does not have sufficient funds or proper authorization.
        let collateral_token: Address = env
            .storage()
            .instance()
            .get(&DataKey::CollateralToken)
            .ok_or(MarketError::StorageCorrupted)?;
        let token_client = token::Client::new(&env, &collateral_token);
        token_client.transfer(&user, &env.current_contract_address(), &cost);

        // Update state
        if outcome == OUTCOME_YES {
            env.storage()
                .instance()
                .set(&DataKey::YesSold, &(q_yes + amount));
        } else {
            env.storage()
                .instance()
                .set(&DataKey::NoSold, &(q_no + amount));
        }

        let pool: i128 = env
            .storage()
            .instance()
            .get(&DataKey::CollateralPool)
            .ok_or(MarketError::StorageCorrupted)?;
        env.storage()
            .instance()
            .set(&DataKey::CollateralPool, &(pool + cost));

        // Update user balance
        let balance_key = DataKey::UserBalance(user.clone(), outcome);
        let current_balance: i128 = env.storage().instance().get(&balance_key).unwrap_or(0);
        env.storage()
            .instance()
            .set(&balance_key, &(current_balance + amount));

        Ok(cost)
    }

    /// Sell outcome tokens.
    ///
    /// # Arguments
    /// * `user` - User selling tokens (must authorize)
    /// * `outcome` - 0 for YES, 1 for NO
    /// * `amount` - Amount of tokens to sell (scaled by 10^7)
    /// * `min_return` - Minimum collateral to receive (slippage protection)
    ///
    /// # Returns
    /// Actual collateral received
    pub fn sell(
        env: Env,
        user: Address,
        outcome: u32,
        amount: i128,
        min_return: i128,
    ) -> Result<i128, MarketError> {
        Self::require_initialized(&env)?;
        Self::require_not_resolved(&env)?;

        if outcome != OUTCOME_YES && outcome != OUTCOME_NO {
            return Err(MarketError::InvalidOutcome);
        }
        if amount <= 0 {
            return Err(MarketError::InvalidAmount);
        }

        user.require_auth();

        // Check user has sufficient balance
        let balance_key = DataKey::UserBalance(user.clone(), outcome);
        let current_balance: i128 = env.storage().instance().get(&balance_key).unwrap_or(0);
        if current_balance < amount {
            return Err(MarketError::InsufficientBalance);
        }

        // Get current state
        let b: i128 = env
            .storage()
            .instance()
            .get(&DataKey::LiquidityParam)
            .ok_or(MarketError::StorageCorrupted)?;
        let q_yes: i128 = env
            .storage()
            .instance()
            .get(&DataKey::YesSold)
            .ok_or(MarketError::StorageCorrupted)?;
        let q_no: i128 = env
            .storage()
            .instance()
            .get(&DataKey::NoSold)
            .ok_or(MarketError::StorageCorrupted)?;

        // Calculate return
        let return_amount = lmsr::calculate_sell_return(q_yes, q_no, amount, outcome, b)?;

        if return_amount < min_return {
            return Err(MarketError::ReturnTooLow);
        }

        // Update state
        if outcome == OUTCOME_YES {
            env.storage()
                .instance()
                .set(&DataKey::YesSold, &(q_yes - amount));
        } else {
            env.storage()
                .instance()
                .set(&DataKey::NoSold, &(q_no - amount));
        }

        let pool: i128 = env
            .storage()
            .instance()
            .get(&DataKey::CollateralPool)
            .ok_or(MarketError::StorageCorrupted)?;

        // Guard against pool underflow (should not happen with correct LMSR math)
        if pool < return_amount {
            return Err(MarketError::InsufficientPool);
        }

        env.storage()
            .instance()
            .set(&DataKey::CollateralPool, &(pool - return_amount));

        // Update user balance
        env.storage()
            .instance()
            .set(&balance_key, &(current_balance - amount));

        // Transfer collateral to user
        // Note: token_client.transfer() may panic on failure (e.g., insufficient balance,
        // authorization issues). These panics are appropriate as they indicate contract
        // state inconsistency or external token contract issues.
        let collateral_token: Address = env
            .storage()
            .instance()
            .get(&DataKey::CollateralToken)
            .ok_or(MarketError::StorageCorrupted)?;
        let token_client = token::Client::new(&env, &collateral_token);
        token_client.transfer(&env.current_contract_address(), &user, &return_amount);

        Ok(return_amount)
    }

    /// Resolve the market (oracle only).
    ///
    /// # Arguments
    /// * `oracle` - Must match the oracle set at initialization
    /// * `winning_outcome` - 0 for YES, 1 for NO
    pub fn resolve(env: Env, oracle: Address, winning_outcome: u32) -> Result<(), MarketError> {
        Self::require_initialized(&env)?;
        Self::require_not_resolved(&env)?;

        if winning_outcome != OUTCOME_YES && winning_outcome != OUTCOME_NO {
            return Err(MarketError::InvalidOutcome);
        }

        // Verify caller is oracle
        let stored_oracle: Address = env
            .storage()
            .instance()
            .get(&DataKey::Oracle)
            .ok_or(MarketError::StorageCorrupted)?;
        if oracle != stored_oracle {
            return Err(MarketError::Unauthorized);
        }
        oracle.require_auth();

        // Mark as resolved
        env.storage().instance().set(&DataKey::Resolved, &true);
        env.storage()
            .instance()
            .set(&DataKey::WinningOutcome, &winning_outcome);

        Ok(())
    }

    /// Claim winnings after market resolution.
    /// Each winning token is redeemable for 1 unit of collateral (1:1 redemption),
    /// minus a 2% fee that stays in the pool (recoverable by oracle via withdraw_remaining).
    /// Note: Losing tokens have zero value and are not claimed.
    ///
    /// # Arguments
    /// * `user` - User claiming (must authorize)
    ///
    /// # Returns
    /// Amount of collateral claimed (after fee deduction)
    pub fn claim(env: Env, user: Address) -> Result<i128, MarketError> {
        Self::require_initialized(&env)?;
        Self::require_resolved(&env)?;

        user.require_auth();

        let winning_outcome: u32 = env
            .storage()
            .instance()
            .get(&DataKey::WinningOutcome)
            .ok_or(MarketError::StorageCorrupted)?;

        // Get user's winning token balance
        let balance_key = DataKey::UserBalance(user.clone(), winning_outcome);
        let winning_balance: i128 = env.storage().instance().get(&balance_key).unwrap_or(0);

        if winning_balance <= 0 {
            return Err(MarketError::NothingToClaim);
        }

        // Each winning token is worth 1 unit of collateral
        let gross_payout = winning_balance;

        // Calculate fee (2% = 200 basis points)
        // Fee stays in pool; oracle recovers via withdraw_remaining()
        // Note: Integer division truncates, so amounts < 50 units have zero fee (dust-level)
        let fee = gross_payout
            .checked_mul(CLAIM_FEE_BPS)
            .ok_or(MarketError::Overflow)?
            .checked_div(BPS_DENOMINATOR)
            .ok_or(MarketError::Overflow)?;
        let user_payout = gross_payout
            .checked_sub(fee)
            .ok_or(MarketError::Overflow)?;

        // Zero out user's balance
        env.storage().instance().set(&balance_key, &0i128);

        // Update collateral pool (only deduct user_payout, fee stays in pool)
        let pool: i128 = env
            .storage()
            .instance()
            .get(&DataKey::CollateralPool)
            .ok_or(MarketError::StorageCorrupted)?;

        // Guard against pool underflow (should not happen with correct market operation)
        if pool < user_payout {
            return Err(MarketError::InsufficientPool);
        }

        env.storage()
            .instance()
            .set(&DataKey::CollateralPool, &(pool - user_payout));

        // Transfer collateral to user (minus fee)
        // Note: token_client.transfer() may panic on failure (e.g., insufficient balance,
        // authorization issues). These panics are appropriate as they indicate contract
        // state inconsistency or external token contract issues.
        let collateral_token: Address = env
            .storage()
            .instance()
            .get(&DataKey::CollateralToken)
            .ok_or(MarketError::StorageCorrupted)?;
        let token_client = token::Client::new(&env, &collateral_token);
        token_client.transfer(&env.current_contract_address(), &user, &user_payout);

        Ok(user_payout)
    }

    /// Withdraw remaining pool after market resolution (oracle only).
    ///
    /// After a market resolves and winners claim their payouts, there may be
    /// leftover funds in the pool (from losing bets). This function allows
    /// the oracle to recover those remaining funds.
    ///
    /// # Arguments
    /// * `oracle` - Must match the oracle set at initialization
    ///
    /// # Returns
    /// Amount of collateral withdrawn
    pub fn withdraw_remaining(env: Env, oracle: Address) -> Result<i128, MarketError> {
        Self::require_initialized(&env)?;
        Self::require_resolved(&env)?;

        // Verify caller is oracle
        let stored_oracle: Address = env
            .storage()
            .instance()
            .get(&DataKey::Oracle)
            .ok_or(MarketError::StorageCorrupted)?;
        if oracle != stored_oracle {
            return Err(MarketError::Unauthorized);
        }
        oracle.require_auth();

        // Get remaining pool
        let pool: i128 = env
            .storage()
            .instance()
            .get(&DataKey::CollateralPool)
            .ok_or(MarketError::StorageCorrupted)?;

        if pool <= 0 {
            return Err(MarketError::NothingToClaim);
        }

        // Zero out the pool
        env.storage().instance().set(&DataKey::CollateralPool, &0i128);

        // Transfer remaining pool to oracle
        let collateral_token: Address = env
            .storage()
            .instance()
            .get(&DataKey::CollateralToken)
            .ok_or(MarketError::StorageCorrupted)?;
        let token_client = token::Client::new(&env, &collateral_token);
        token_client.transfer(&env.current_contract_address(), &oracle, &pool);

        Ok(pool)
    }

    /// Get the current price of an outcome.
    ///
    /// # Returns
    /// Price scaled by 10^7 (5_000_000 = 0.5 = 50%)
    pub fn get_price(env: Env, outcome: u32) -> Result<i128, MarketError> {
        Self::require_initialized(&env)?;

        let b: i128 = env
            .storage()
            .instance()
            .get(&DataKey::LiquidityParam)
            .ok_or(MarketError::StorageCorrupted)?;
        let q_yes: i128 = env
            .storage()
            .instance()
            .get(&DataKey::YesSold)
            .ok_or(MarketError::StorageCorrupted)?;
        let q_no: i128 = env
            .storage()
            .instance()
            .get(&DataKey::NoSold)
            .ok_or(MarketError::StorageCorrupted)?;

        lmsr::calculate_price(q_yes, q_no, outcome, b)
    }

    /// Get a quote for buying tokens.
    ///
    /// # Returns
    /// (cost, price_after) both scaled by 10^7
    pub fn get_quote(env: Env, outcome: u32, amount: i128) -> Result<(i128, i128), MarketError> {
        Self::require_initialized(&env)?;
        Self::require_not_resolved(&env)?;

        if outcome != OUTCOME_YES && outcome != OUTCOME_NO {
            return Err(MarketError::InvalidOutcome);
        }
        if amount <= 0 {
            return Err(MarketError::InvalidAmount);
        }

        let b: i128 = env
            .storage()
            .instance()
            .get(&DataKey::LiquidityParam)
            .ok_or(MarketError::StorageCorrupted)?;
        let q_yes: i128 = env
            .storage()
            .instance()
            .get(&DataKey::YesSold)
            .ok_or(MarketError::StorageCorrupted)?;
        let q_no: i128 = env
            .storage()
            .instance()
            .get(&DataKey::NoSold)
            .ok_or(MarketError::StorageCorrupted)?;

        let cost = lmsr::calculate_buy_cost(q_yes, q_no, amount, outcome, b)?;

        // Calculate price after purchase
        let (new_q_yes, new_q_no) = if outcome == OUTCOME_YES {
            (q_yes + amount, q_no)
        } else {
            (q_yes, q_no + amount)
        };

        let price_after = lmsr::calculate_price(new_q_yes, new_q_no, outcome, b)?;

        Ok((cost, price_after))
    }

    /// Get a quote for selling tokens.
    ///
    /// # Returns
    /// (return_amount, price_after) both scaled by 10^7
    pub fn get_sell_quote(
        env: Env,
        outcome: u32,
        amount: i128,
    ) -> Result<(i128, i128), MarketError> {
        Self::require_initialized(&env)?;
        Self::require_not_resolved(&env)?;

        if outcome != OUTCOME_YES && outcome != OUTCOME_NO {
            return Err(MarketError::InvalidOutcome);
        }
        if amount <= 0 {
            return Err(MarketError::InvalidAmount);
        }

        let b: i128 = env
            .storage()
            .instance()
            .get(&DataKey::LiquidityParam)
            .ok_or(MarketError::StorageCorrupted)?;
        let q_yes: i128 = env
            .storage()
            .instance()
            .get(&DataKey::YesSold)
            .ok_or(MarketError::StorageCorrupted)?;
        let q_no: i128 = env
            .storage()
            .instance()
            .get(&DataKey::NoSold)
            .ok_or(MarketError::StorageCorrupted)?;

        let return_amount = lmsr::calculate_sell_return(q_yes, q_no, amount, outcome, b)?;

        // Calculate price after sale
        let (new_q_yes, new_q_no) = if outcome == OUTCOME_YES {
            (q_yes - amount, q_no)
        } else {
            (q_yes, q_no - amount)
        };

        let price_after = lmsr::calculate_price(new_q_yes, new_q_no, outcome, b)?;

        Ok((return_amount, price_after))
    }

    /// Get user's token balance for an outcome.
    pub fn get_balance(env: Env, user: Address, outcome: u32) -> i128 {
        let balance_key = DataKey::UserBalance(user, outcome);
        env.storage().instance().get(&balance_key).unwrap_or(0)
    }

    /// Get market state.
    ///
    /// # Returns
    /// (yes_sold, no_sold, collateral_pool, is_resolved)
    pub fn get_state(env: Env) -> Result<(i128, i128, i128, bool), MarketError> {
        Self::require_initialized(&env)?;

        let q_yes: i128 = env
            .storage()
            .instance()
            .get(&DataKey::YesSold)
            .ok_or(MarketError::StorageCorrupted)?;
        let q_no: i128 = env
            .storage()
            .instance()
            .get(&DataKey::NoSold)
            .ok_or(MarketError::StorageCorrupted)?;
        let pool: i128 = env
            .storage()
            .instance()
            .get(&DataKey::CollateralPool)
            .ok_or(MarketError::StorageCorrupted)?;
        let resolved: bool = env
            .storage()
            .instance()
            .get(&DataKey::Resolved)
            .ok_or(MarketError::StorageCorrupted)?;

        Ok((q_yes, q_no, pool, resolved))
    }

    /// Get the oracle address.
    pub fn get_oracle(env: Env) -> Result<Address, MarketError> {
        Self::require_initialized(&env)?;
        env.storage()
            .instance()
            .get(&DataKey::Oracle)
            .ok_or(MarketError::StorageCorrupted)
    }

    /// Get the liquidity parameter.
    pub fn get_liquidity_param(env: Env) -> Result<i128, MarketError> {
        Self::require_initialized(&env)?;
        env.storage()
            .instance()
            .get(&DataKey::LiquidityParam)
            .ok_or(MarketError::StorageCorrupted)
    }

    /// Get the winning outcome (only valid after resolution).
    pub fn get_winning_outcome(env: Env) -> Result<u32, MarketError> {
        Self::require_initialized(&env)?;
        Self::require_resolved(&env)?;
        env.storage()
            .instance()
            .get(&DataKey::WinningOutcome)
            .ok_or(MarketError::StorageCorrupted)
    }

    /// Get the metadata hash (IPFS CID for market metadata JSON).
    pub fn get_metadata_hash(env: Env) -> Result<String, MarketError> {
        Self::require_initialized(&env)?;
        env.storage()
            .instance()
            .get(&DataKey::MetadataHash)
            .ok_or(MarketError::StorageCorrupted)
    }

    /// Get the collateral token address.
    pub fn get_collateral_token(env: Env) -> Result<Address, MarketError> {
        Self::require_initialized(&env)?;
        env.storage()
            .instance()
            .get(&DataKey::CollateralToken)
            .ok_or(MarketError::StorageCorrupted)
    }

    // --- Internal helpers ---

    fn require_initialized(env: &Env) -> Result<(), MarketError> {
        if !env.storage().instance().has(&DataKey::Oracle) {
            return Err(MarketError::NotInitialized);
        }
        Ok(())
    }

    fn require_not_resolved(env: &Env) -> Result<(), MarketError> {
        let resolved: bool = env
            .storage()
            .instance()
            .get(&DataKey::Resolved)
            .ok_or(MarketError::StorageCorrupted)?;
        if resolved {
            return Err(MarketError::AlreadyResolved);
        }
        Ok(())
    }

    fn require_resolved(env: &Env) -> Result<(), MarketError> {
        let resolved: bool = env
            .storage()
            .instance()
            .get(&DataKey::Resolved)
            .ok_or(MarketError::StorageCorrupted)?;
        if !resolved {
            return Err(MarketError::NotResolved);
        }
        Ok(())
    }
}

#[cfg(test)]
mod test {
    use super::*;
    use soroban_sdk::{testutils::Address as _, token::StellarAssetClient, Env};

    /// Set up token and oracle, then register initialized market contract.
    /// Returns (env, contract_id, oracle, token_address)
    fn setup_test() -> (Env, Address, Address, Address) {
        setup_test_with_params(100 * SCALE_FACTOR, 70 * SCALE_FACTOR)
    }

    /// Set up with custom liquidity and funding params.
    fn setup_test_with_params(liquidity_param: i128, initial_funding: i128) -> (Env, Address, Address, Address) {
        let env = Env::default();
        env.mock_all_auths();

        let oracle = Address::generate(&env);

        // Create a test token
        let token_admin = Address::generate(&env);
        let token_contract = env.register_stellar_asset_contract_v2(token_admin.clone());
        let token_address = token_contract.address();
        let token_admin_client = StellarAssetClient::new(&env, &token_address);

        // Mint tokens to oracle for initial funding
        token_admin_client.mint(&oracle, &(1000 * SCALE_FACTOR));

        // Register contract with constructor args (this calls __constructor which calls initialize)
        let contract_id = env.register(
            LmsrMarket,
            (
                oracle.clone(),
                token_address.clone(),
                liquidity_param,
                String::from_str(&env, "QmTest"),
                initial_funding,
            ),
        );

        (env, contract_id, oracle, token_address)
    }

    #[test]
    fn test_initialize() {
        // setup_test() now registers with constructor which initializes
        let (env, contract_id, oracle, _token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let b = 100 * SCALE_FACTOR;

        // Verify initialization worked
        assert_eq!(client.get_oracle(), oracle);
        assert_eq!(client.get_liquidity_param(), b);
    }

    #[test]
    fn test_buy() {
        let (env, contract_id, _oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        // Create a user and mint tokens
        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // Buy YES tokens
        let amount = 10 * SCALE_FACTOR;
        let max_cost = 50 * SCALE_FACTOR;
        let cost = client.buy(&user, &0, &amount, &max_cost);

        assert!(cost > 0);
        assert_eq!(client.get_balance(&user, &0), amount);
    }

    #[test]
    fn test_resolve_and_claim() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        // User buys YES tokens
        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        let amount = 10 * SCALE_FACTOR;
        client.buy(&user, &0, &amount, &(50 * SCALE_FACTOR));

        // Resolve market with YES winning
        client.resolve(&oracle, &0);

        // User claims winnings (minus 2% fee)
        let payout = client.claim(&user);
        let expected_payout = amount - (amount * CLAIM_FEE_BPS / BPS_DENOMINATOR);
        assert_eq!(payout, expected_payout);
    }

    #[test]
    fn test_price_at_equilibrium() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        // At equilibrium (no tokens sold), price should be ~0.5
        let price_yes = client.get_price(&0);
        let price_no = client.get_price(&1);

        // Allow for some floating point error
        assert!(
            price_yes >= 4_900_000 && price_yes <= 5_100_000,
            "Expected price_yes near 5_000_000, got {}",
            price_yes
        );
        assert!(
            price_no >= 4_900_000 && price_no <= 5_100_000,
            "Expected price_no near 5_000_000, got {}",
            price_no
        );
    }

    // --- Authorization tests ---

    #[test]
    #[should_panic(expected = "Error(Contract, #10)")] // Unauthorized = 10
    fn test_resolve_by_non_oracle_fails() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        // Try to resolve with non-oracle address
        let attacker = Address::generate(&env);
        client.resolve(&attacker, &0); // Should panic with Unauthorized
    }

    // --- Double-claim prevention tests ---

    #[test]
    #[should_panic(expected = "Error(Contract, #13)")] // NothingToClaim = 13
    fn test_double_claim_fails() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        // User buys YES tokens
        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        let amount = 10 * SCALE_FACTOR;
        client.buy(&user, &0, &amount, &(50 * SCALE_FACTOR));

        // Resolve market with YES winning
        client.resolve(&oracle, &0);

        // First claim succeeds
        client.claim(&user);

        // Second claim should fail
        client.claim(&user); // Should panic with NothingToClaim
    }

    // --- Slippage protection tests ---

    #[test]
    #[should_panic(expected = "Error(Contract, #8)")] // SlippageExceeded = 8
    fn test_buy_slippage_exceeded() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // Try to buy with max_cost = 0 (impossible)
        let amount = 10 * SCALE_FACTOR;
        client.buy(&user, &0, &amount, &0); // Should panic with SlippageExceeded
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #9)")] // ReturnTooLow = 9
    fn test_sell_min_return_not_met() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // First buy some tokens
        let amount = 10 * SCALE_FACTOR;
        client.buy(&user, &0, &amount, &(50 * SCALE_FACTOR));

        // Try to sell with impossibly high min_return
        client.sell(&user, &0, &amount, &(i128::MAX / 2)); // Should panic with ReturnTooLow
    }

    // --- Sell function tests ---

    #[test]
    fn test_sell_basic() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // Buy tokens
        let amount = 10 * SCALE_FACTOR;
        let buy_cost = client.buy(&user, &0, &amount, &(50 * SCALE_FACTOR));
        assert_eq!(client.get_balance(&user, &0), amount);

        // Sell half the tokens
        let sell_amount = 5 * SCALE_FACTOR;
        let sell_return = client.sell(&user, &0, &sell_amount, &0);
        assert!(sell_return > 0, "Sell return should be positive");
        assert_eq!(client.get_balance(&user, &0), amount - sell_amount);

        // Sell return should be less than buy cost (round-trip loss expected)
        assert!(
            sell_return < buy_cost,
            "Selling should return less than buying cost due to price movement"
        );
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #7)")] // InsufficientBalance = 7
    fn test_sell_insufficient_balance() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // Buy a small amount
        client.buy(&user, &0, &(5 * SCALE_FACTOR), &(50 * SCALE_FACTOR));

        // Try to sell more than owned
        client.sell(&user, &0, &(10 * SCALE_FACTOR), &0); // Should panic
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #3)")] // AlreadyResolved = 3
    fn test_sell_after_resolution_fails() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // Buy tokens
        client.buy(&user, &0, &(10 * SCALE_FACTOR), &(50 * SCALE_FACTOR));

        // Resolve market
        client.resolve(&oracle, &0);

        // Try to sell after resolution
        client.sell(&user, &0, &(5 * SCALE_FACTOR), &0); // Should panic
    }

    // --- Market lifecycle error state tests ---

    // Note: test_buy_on_uninitialized_contract was removed because with the constructor,
    // contracts are always initialized at deployment time.

    #[test]
    #[should_panic(expected = "Error(Contract, #3)")] // AlreadyResolved = 3
    fn test_buy_on_resolved_market() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        // Resolve immediately
        client.resolve(&oracle, &0);

        // Try to buy after resolution
        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));
        client.buy(&user, &0, &(10 * SCALE_FACTOR), &(50 * SCALE_FACTOR));
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #4)")] // NotResolved = 4
    fn test_claim_on_unresolved_market() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // Buy tokens but don't resolve
        client.buy(&user, &0, &(10 * SCALE_FACTOR), &(50 * SCALE_FACTOR));

        // Try to claim without resolution
        client.claim(&user); // Should panic
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #13)")] // NothingToClaim = 13
    fn test_loser_cannot_claim() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // User buys NO tokens
        client.buy(&user, &1, &(10 * SCALE_FACTOR), &(50 * SCALE_FACTOR));

        // Resolve market with YES winning (user loses)
        client.resolve(&oracle, &0);

        // Loser tries to claim
        client.claim(&user); // Should panic with NothingToClaim
    }

    // --- Withdraw remaining tests ---

    #[test]
    fn test_withdraw_remaining_after_claims() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let winner = Address::generate(&env);
        let loser = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&winner, &(100 * SCALE_FACTOR));
        token_admin_client.mint(&loser, &(100 * SCALE_FACTOR));

        // Winner buys YES, loser buys NO
        let yes_cost = client.buy(&winner, &0, &(10 * SCALE_FACTOR), &(50 * SCALE_FACTOR));
        let no_cost = client.buy(&loser, &1, &(10 * SCALE_FACTOR), &(50 * SCALE_FACTOR));

        // Check pool increased (initial_funding is 70 * SCALE_FACTOR from setup_test)
        let (_, _, pool_before, _) = client.get_state();
        assert_eq!(pool_before, 70 * SCALE_FACTOR + yes_cost + no_cost);

        // Resolve: YES wins
        client.resolve(&oracle, &0);

        // Winner claims (minus 2% fee)
        let payout = client.claim(&winner);
        let expected_payout = 10 * SCALE_FACTOR - (10 * SCALE_FACTOR * CLAIM_FEE_BPS / BPS_DENOMINATOR);
        assert_eq!(payout, expected_payout); // 1:1 redemption minus 2% fee

        // Pool should have remaining funds (loser's money + part of initial funding)
        let (_, _, pool_after_claim, _) = client.get_state();
        assert!(pool_after_claim > 0, "Pool should have remaining funds");

        // Oracle withdraws remaining
        let withdrawn = client.withdraw_remaining(&oracle);
        assert_eq!(withdrawn, pool_after_claim);

        // Pool should be zero now
        let (_, _, pool_final, _) = client.get_state();
        assert_eq!(pool_final, 0);
    }

    #[test]
    fn test_withdraw_remaining_no_trades() {
        let (env, contract_id, oracle, _token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        // No trades, just resolve
        client.resolve(&oracle, &0);

        // Oracle can withdraw entire initial funding (70 * SCALE_FACTOR from setup_test)
        let withdrawn = client.withdraw_remaining(&oracle);
        assert_eq!(withdrawn, 70 * SCALE_FACTOR);
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #4)")] // NotResolved = 4
    fn test_withdraw_remaining_before_resolve() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        // Try to withdraw before resolution
        client.withdraw_remaining(&oracle); // Should panic
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #10)")] // Unauthorized = 10
    fn test_withdraw_remaining_non_oracle() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        client.resolve(&oracle, &0);

        // Non-oracle tries to withdraw
        let attacker = Address::generate(&env);
        client.withdraw_remaining(&attacker); // Should panic
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #13)")] // NothingToClaim = 13
    fn test_withdraw_remaining_twice() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        client.resolve(&oracle, &0);

        // First withdrawal succeeds
        client.withdraw_remaining(&oracle);

        // Second withdrawal should fail
        client.withdraw_remaining(&oracle); // Should panic
    }

    // --- Quote validation tests ---

    #[test]
    #[should_panic(expected = "Error(Contract, #5)")] // InvalidOutcome = 5
    fn test_get_quote_invalid_outcome() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        // Try to get quote with invalid outcome
        client.get_quote(&99, &(10 * SCALE_FACTOR));
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #6)")] // InvalidAmount = 6
    fn test_get_quote_zero_amount() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        // Try to get quote with zero amount
        client.get_quote(&0, &0);
    }

    // --- Double resolution prevention test ---

    #[test]
    #[should_panic(expected = "Error(Contract, #3)")] // AlreadyResolved = 3
    fn test_double_resolve_fails() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        // First resolution succeeds
        client.resolve(&oracle, &0);

        // Second resolution should fail
        client.resolve(&oracle, &1); // Should panic with AlreadyResolved
    }

    // --- Insufficient initial funding test ---

    #[test]
    #[should_panic(expected = "InvalidAmount")]
    fn test_initialize_insufficient_funding() {
        let env = Env::default();
        env.mock_all_auths();

        let oracle = Address::generate(&env);

        // Create a test token
        let token_admin = Address::generate(&env);
        let token_contract = env.register_stellar_asset_contract_v2(token_admin.clone());
        let token_address = token_contract.address();
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&oracle, &(1000 * SCALE_FACTOR));

        let b = 100 * SCALE_FACTOR;
        // Required funding is b * ln(2) ≈ 69.3, so 50 is insufficient
        let insufficient_funding = 50 * SCALE_FACTOR;

        // Constructor should panic with InvalidAmount
        let _contract_id = env.register(
            LmsrMarket,
            (
                oracle.clone(),
                token_address.clone(),
                b,
                String::from_str(&env, "QmTest"),
                insufficient_funding,
            ),
        );
    }

    // --- Invalid outcome tests for buy/sell/resolve ---

    #[test]
    #[should_panic(expected = "Error(Contract, #5)")] // InvalidOutcome = 5
    fn test_buy_invalid_outcome() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // Try to buy with invalid outcome (99)
        client.buy(&user, &99, &(10 * SCALE_FACTOR), &(50 * SCALE_FACTOR));
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #5)")] // InvalidOutcome = 5
    fn test_sell_invalid_outcome() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // First buy some valid tokens
        client.buy(&user, &0, &(10 * SCALE_FACTOR), &(50 * SCALE_FACTOR));

        // Try to sell with invalid outcome (99)
        client.sell(&user, &99, &(5 * SCALE_FACTOR), &0);
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #5)")] // InvalidOutcome = 5
    fn test_resolve_invalid_outcome() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        // Try to resolve with invalid outcome (99)
        client.resolve(&oracle, &99);
    }

    // --- Zero/negative amount tests ---

    #[test]
    #[should_panic(expected = "Error(Contract, #6)")] // InvalidAmount = 6
    fn test_buy_zero_amount() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // Try to buy zero amount
        client.buy(&user, &0, &0, &(50 * SCALE_FACTOR));
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #6)")] // InvalidAmount = 6
    fn test_buy_negative_amount() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // Try to buy negative amount
        client.buy(&user, &0, &(-10 * SCALE_FACTOR), &(50 * SCALE_FACTOR));
    }

    // --- Multiple users scenario ---

    #[test]
    fn test_multiple_users_claim_correctly() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let token_admin_client = StellarAssetClient::new(&env, &token_address);

        // User1 buys YES, User2 buys NO
        let user1 = Address::generate(&env);
        let user2 = Address::generate(&env);
        token_admin_client.mint(&user1, &(100 * SCALE_FACTOR));
        token_admin_client.mint(&user2, &(100 * SCALE_FACTOR));

        let amount1 = 10 * SCALE_FACTOR;
        let amount2 = 8 * SCALE_FACTOR;
        client.buy(&user1, &0, &amount1, &(50 * SCALE_FACTOR)); // YES
        client.buy(&user2, &1, &amount2, &(50 * SCALE_FACTOR)); // NO

        // Resolve with YES winning
        client.resolve(&oracle, &0);

        // User1 (winner) claims successfully (minus 2% fee)
        let payout1 = client.claim(&user1);
        let expected_payout1 = amount1 - (amount1 * CLAIM_FEE_BPS / BPS_DENOMINATOR);
        assert_eq!(payout1, expected_payout1);

        // User2's balance should be 0 for winning outcome
        assert_eq!(client.get_balance(&user2, &0), 0);
    }

    // --- Claim fee tests ---

    #[test]
    fn test_claim_fee_calculation() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let token_admin_client = StellarAssetClient::new(&env, &token_address);

        let user = Address::generate(&env);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // Buy exactly 100 tokens (for easy math)
        let amount = 100 * SCALE_FACTOR;
        client.buy(&user, &0, &amount, &(100 * SCALE_FACTOR));

        let (_, _, pool_before, _) = client.get_state();

        // Resolve: YES wins
        client.resolve(&oracle, &0);

        // Claim and verify 2% fee
        let payout = client.claim(&user);

        // 2% of 100 = 2, so payout should be 98
        let expected_fee = 2 * SCALE_FACTOR;
        let expected_payout = amount - expected_fee;
        assert_eq!(payout, expected_payout, "Expected 98% payout after 2% fee");

        // Verify fee stayed in pool
        let (_, _, pool_after, _) = client.get_state();
        // Pool should have: pool_before - payout = pool_before - (amount - fee) = pool_before - amount + fee
        // Since winning tokens are 1:1 redeemable from pool, the fee portion remains
        assert_eq!(
            pool_after,
            pool_before - expected_payout,
            "Fee should remain in pool"
        );
    }

    #[test]
    fn test_oracle_collects_accumulated_fees_from_multiple_claims() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let token_admin_client = StellarAssetClient::new(&env, &token_address);

        // Three users: two winners (YES), one loser (NO)
        let winner1 = Address::generate(&env);
        let winner2 = Address::generate(&env);
        let loser = Address::generate(&env);
        token_admin_client.mint(&winner1, &(100 * SCALE_FACTOR));
        token_admin_client.mint(&winner2, &(100 * SCALE_FACTOR));
        token_admin_client.mint(&loser, &(100 * SCALE_FACTOR));

        // Winner1 buys 100 YES, Winner2 buys 50 YES, Loser buys 30 NO
        let amount1 = 100 * SCALE_FACTOR;
        let amount2 = 50 * SCALE_FACTOR;
        let amount3 = 30 * SCALE_FACTOR;
        client.buy(&winner1, &0, &amount1, &(100 * SCALE_FACTOR));
        client.buy(&winner2, &0, &amount2, &(100 * SCALE_FACTOR));
        client.buy(&loser, &1, &amount3, &(50 * SCALE_FACTOR));

        let (_, _, _pool_after_buys, _) = client.get_state();

        // Resolve: YES wins
        client.resolve(&oracle, &0);

        // Both winners claim
        let payout1 = client.claim(&winner1);
        let payout2 = client.claim(&winner2);

        // Verify payouts are 98% of token amounts
        let expected_fee1 = amount1 * CLAIM_FEE_BPS / BPS_DENOMINATOR; // 2% of 100 = 2
        let expected_fee2 = amount2 * CLAIM_FEE_BPS / BPS_DENOMINATOR; // 2% of 50 = 1
        assert_eq!(payout1, amount1 - expected_fee1);
        assert_eq!(payout2, amount2 - expected_fee2);

        // Total fees accumulated: 2 + 1 = 3 SCALE_FACTOR
        let total_fees = expected_fee1 + expected_fee2;

        // Pool should have: initial_funding + all_buy_costs - winner_payouts
        // Which includes: loser's bet + accumulated fees
        let (_, _, pool_after_claims, _) = client.get_state();
        assert!(pool_after_claims > 0, "Pool should have remaining funds");

        // Oracle withdraws everything remaining
        let withdrawn = client.withdraw_remaining(&oracle);
        assert_eq!(withdrawn, pool_after_claims);

        // Verify oracle got at least the accumulated fees
        // (also includes loser's funds and any leftover from initial funding)
        assert!(
            withdrawn >= total_fees,
            "Oracle should receive at least the accumulated fees: {} >= {}",
            withdrawn,
            total_fees
        );

        // Pool should be empty now
        let (_, _, pool_final, _) = client.get_state();
        assert_eq!(pool_final, 0);
    }

    #[test]
    fn test_claim_fee_small_amount_truncation() {
        // Documents expected behavior: fee rounds to 0 for tiny amounts
        // due to integer division truncation.
        // This is acceptable as these are dust-level amounts.
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let token_admin_client = StellarAssetClient::new(&env, &token_address);

        let user = Address::generate(&env);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // Buy 49 units (below threshold where fee = 0)
        // Fee calculation: 49 * 200 / 10000 = 9800 / 10000 = 0 (integer truncation)
        let tiny_amount: i128 = 49;
        client.buy(&user, &0, &tiny_amount, &SCALE_FACTOR);

        client.resolve(&oracle, &0);

        let payout = client.claim(&user);

        // With amount = 49, fee = 49 * 200 / 10000 = 0 (truncated)
        // So user gets full amount back (no fee on dust amounts)
        assert_eq!(
            payout, tiny_amount,
            "Dust amounts should have zero fee due to integer truncation"
        );
    }

    // --- Sell all tokens to equilibrium ---

    #[test]
    fn test_sell_all_returns_to_near_equilibrium() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(200 * SCALE_FACTOR));

        // Buy tokens
        let amount = 10 * SCALE_FACTOR;
        let buy_cost = client.buy(&user, &0, &amount, &(50 * SCALE_FACTOR));

        // Get state after buy
        let (q_yes_after_buy, _, _, _) = client.get_state();
        assert_eq!(q_yes_after_buy, amount);

        // Sell all back
        let sell_return = client.sell(&user, &0, &amount, &0);

        // Get state after sell
        let (q_yes_after_sell, q_no_after_sell, _, _) = client.get_state();
        assert_eq!(q_yes_after_sell, 0);
        assert_eq!(q_no_after_sell, 0);

        // User balance should be 0
        assert_eq!(client.get_balance(&user, &0), 0);

        // LMSR is symmetric: buying and immediately selling the same amount
        // returns exactly what was paid (no spread). The sell_return should
        // equal buy_cost when returning to the same state.
        assert_eq!(
            sell_return, buy_cost,
            "LMSR symmetric round-trip: sell_return={}, buy_cost={}",
            sell_return, buy_cost
        );
    }

    // --- get_sell_quote tests ---

    #[test]
    fn test_get_sell_quote_basic() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        // Buy tokens first
        let amount = 10 * SCALE_FACTOR;
        client.buy(&user, &0, &amount, &(50 * SCALE_FACTOR));

        // Get sell quote
        let (return_amount, price_after) = client.get_sell_quote(&0, &amount);

        assert!(return_amount > 0, "Sell return should be positive");
        assert!(
            price_after >= 0 && price_after <= SCALE_FACTOR,
            "Price after should be in [0, 1]"
        );
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #5)")] // InvalidOutcome = 5
    fn test_get_sell_quote_invalid_outcome() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        // Try to get sell quote with invalid outcome
        client.get_sell_quote(&99, &(10 * SCALE_FACTOR));
    }
}
