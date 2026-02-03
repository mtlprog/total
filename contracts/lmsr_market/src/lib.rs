#![no_std]

mod error;
mod lmsr;
mod storage;

use error::MarketError;
use soroban_sdk::{contract, contractimpl, token, Address, BytesN, Env, String};
use storage::{DataKey, OUTCOME_NO, OUTCOME_YES, SCALE_FACTOR};

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
    /// Initialize the market with oracle, collateral token, liquidity parameter, and metadata.
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

        // Oracle must authorize the initialization (they provide initial funding)
        oracle.require_auth();

        // Transfer initial funding from oracle to contract
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
    /// * `max_cost` - Maximum collateral willing to pay (slippage protection)
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
        let b: i128 = env.storage().instance().get(&DataKey::LiquidityParam).unwrap();
        let q_yes: i128 = env.storage().instance().get(&DataKey::YesSold).unwrap();
        let q_no: i128 = env.storage().instance().get(&DataKey::NoSold).unwrap();

        // Calculate cost
        let cost = lmsr::calculate_buy_cost(q_yes, q_no, amount, outcome, b)?;

        if cost > max_cost {
            return Err(MarketError::SlippageExceeded);
        }

        // Transfer collateral from user to contract
        let collateral_token: Address = env
            .storage()
            .instance()
            .get(&DataKey::CollateralToken)
            .unwrap();
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
            .unwrap();
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
        let b: i128 = env.storage().instance().get(&DataKey::LiquidityParam).unwrap();
        let q_yes: i128 = env.storage().instance().get(&DataKey::YesSold).unwrap();
        let q_no: i128 = env.storage().instance().get(&DataKey::NoSold).unwrap();

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
            .unwrap();
        env.storage()
            .instance()
            .set(&DataKey::CollateralPool, &(pool - return_amount));

        // Update user balance
        env.storage()
            .instance()
            .set(&balance_key, &(current_balance - amount));

        // Transfer collateral to user
        let collateral_token: Address = env
            .storage()
            .instance()
            .get(&DataKey::CollateralToken)
            .unwrap();
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
        let stored_oracle: Address = env.storage().instance().get(&DataKey::Oracle).unwrap();
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
    /// Each winning token is redeemable for 1 unit of collateral.
    ///
    /// # Arguments
    /// * `user` - User claiming (must authorize)
    ///
    /// # Returns
    /// Amount of collateral claimed
    pub fn claim(env: Env, user: Address) -> Result<i128, MarketError> {
        Self::require_initialized(&env)?;
        Self::require_resolved(&env)?;

        user.require_auth();

        let winning_outcome: u32 = env
            .storage()
            .instance()
            .get(&DataKey::WinningOutcome)
            .unwrap();

        // Get user's winning token balance
        let balance_key = DataKey::UserBalance(user.clone(), winning_outcome);
        let winning_balance: i128 = env.storage().instance().get(&balance_key).unwrap_or(0);

        if winning_balance <= 0 {
            return Err(MarketError::NothingToClaim);
        }

        // Each winning token is worth 1 unit of collateral
        let payout = winning_balance;

        // Zero out user's balance
        env.storage().instance().set(&balance_key, &0i128);

        // Update collateral pool
        let pool: i128 = env
            .storage()
            .instance()
            .get(&DataKey::CollateralPool)
            .unwrap();
        env.storage()
            .instance()
            .set(&DataKey::CollateralPool, &(pool - payout));

        // Transfer collateral to user
        let collateral_token: Address = env
            .storage()
            .instance()
            .get(&DataKey::CollateralToken)
            .unwrap();
        let token_client = token::Client::new(&env, &collateral_token);
        token_client.transfer(&env.current_contract_address(), &user, &payout);

        Ok(payout)
    }

    /// Get the current price of an outcome.
    ///
    /// # Returns
    /// Price scaled by 10^7 (5_000_000 = 0.5 = 50%)
    pub fn get_price(env: Env, outcome: u32) -> Result<i128, MarketError> {
        Self::require_initialized(&env)?;

        let b: i128 = env.storage().instance().get(&DataKey::LiquidityParam).unwrap();
        let q_yes: i128 = env.storage().instance().get(&DataKey::YesSold).unwrap();
        let q_no: i128 = env.storage().instance().get(&DataKey::NoSold).unwrap();

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

        let b: i128 = env.storage().instance().get(&DataKey::LiquidityParam).unwrap();
        let q_yes: i128 = env.storage().instance().get(&DataKey::YesSold).unwrap();
        let q_no: i128 = env.storage().instance().get(&DataKey::NoSold).unwrap();

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

        let q_yes: i128 = env.storage().instance().get(&DataKey::YesSold).unwrap();
        let q_no: i128 = env.storage().instance().get(&DataKey::NoSold).unwrap();
        let pool: i128 = env
            .storage()
            .instance()
            .get(&DataKey::CollateralPool)
            .unwrap();
        let resolved: bool = env.storage().instance().get(&DataKey::Resolved).unwrap();

        Ok((q_yes, q_no, pool, resolved))
    }

    /// Get the oracle address.
    pub fn get_oracle(env: Env) -> Result<Address, MarketError> {
        Self::require_initialized(&env)?;
        Ok(env.storage().instance().get(&DataKey::Oracle).unwrap())
    }

    /// Get the liquidity parameter.
    pub fn get_liquidity_param(env: Env) -> Result<i128, MarketError> {
        Self::require_initialized(&env)?;
        Ok(env
            .storage()
            .instance()
            .get(&DataKey::LiquidityParam)
            .unwrap())
    }

    /// Get the winning outcome (only valid after resolution).
    pub fn get_winning_outcome(env: Env) -> Result<u32, MarketError> {
        Self::require_initialized(&env)?;
        Self::require_resolved(&env)?;
        Ok(env
            .storage()
            .instance()
            .get(&DataKey::WinningOutcome)
            .unwrap())
    }

    // --- Internal helpers ---

    fn require_initialized(env: &Env) -> Result<(), MarketError> {
        if !env.storage().instance().has(&DataKey::Oracle) {
            return Err(MarketError::NotInitialized);
        }
        Ok(())
    }

    fn require_not_resolved(env: &Env) -> Result<(), MarketError> {
        let resolved: bool = env.storage().instance().get(&DataKey::Resolved).unwrap();
        if resolved {
            return Err(MarketError::AlreadyResolved);
        }
        Ok(())
    }

    fn require_resolved(env: &Env) -> Result<(), MarketError> {
        let resolved: bool = env.storage().instance().get(&DataKey::Resolved).unwrap();
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

    fn setup_test() -> (Env, Address, Address, Address) {
        let env = Env::default();
        env.mock_all_auths();

        let contract_id = env.register(LmsrMarket, ());
        let oracle = Address::generate(&env);

        // Create a test token
        let token_admin = Address::generate(&env);
        let token_contract = env.register_stellar_asset_contract_v2(token_admin.clone());
        let token_address = token_contract.address();
        let token_admin_client = StellarAssetClient::new(&env, &token_address);

        // Mint tokens to oracle for initial funding
        token_admin_client.mint(&oracle, &(1000 * SCALE_FACTOR));

        (env, contract_id, oracle, token_address)
    }

    #[test]
    fn test_initialize() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let b = 100 * SCALE_FACTOR;
        let initial_funding = 70 * SCALE_FACTOR; // > b * ln(2) ≈ 69.3

        client.initialize(
            &oracle,
            &token_address,
            &b,
            &String::from_str(&env, "QmTest"),
            &initial_funding,
        );

        assert_eq!(client.get_oracle(), oracle);
        assert_eq!(client.get_liquidity_param(), b);
    }

    #[test]
    fn test_buy() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let b = 100 * SCALE_FACTOR;
        let initial_funding = 70 * SCALE_FACTOR;

        client.initialize(
            &oracle,
            &token_address,
            &b,
            &String::from_str(&env, "QmTest"),
            &initial_funding,
        );

        // Create a user and mint tokens
        let user = Address::generate(&env);
        let token_admin = Address::generate(&env);
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

        let b = 100 * SCALE_FACTOR;
        let initial_funding = 70 * SCALE_FACTOR;

        client.initialize(
            &oracle,
            &token_address,
            &b,
            &String::from_str(&env, "QmTest"),
            &initial_funding,
        );

        // User buys YES tokens
        let user = Address::generate(&env);
        let token_admin_client = StellarAssetClient::new(&env, &token_address);
        token_admin_client.mint(&user, &(100 * SCALE_FACTOR));

        let amount = 10 * SCALE_FACTOR;
        client.buy(&user, &0, &amount, &(50 * SCALE_FACTOR));

        // Resolve market with YES winning
        client.resolve(&oracle, &0);

        // User claims winnings
        let payout = client.claim(&user);
        assert_eq!(payout, amount);
    }

    #[test]
    fn test_price_at_equilibrium() {
        let (env, contract_id, oracle, token_address) = setup_test();
        let client = LmsrMarketClient::new(&env, &contract_id);

        let b = 100 * SCALE_FACTOR;
        let initial_funding = 70 * SCALE_FACTOR;

        client.initialize(
            &oracle,
            &token_address,
            &b,
            &String::from_str(&env, "QmTest"),
            &initial_funding,
        );

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
}
