#![no_std]

use soroban_sdk::{
    contract, contractimpl, contracterror, contracttype, Address, BytesN, Env, String, Vec,
};

#[contracterror]
#[derive(Copy, Clone, Debug, Eq, PartialEq, PartialOrd, Ord)]
#[repr(u32)]
pub enum FactoryError {
    /// Factory already initialized
    AlreadyInitialized = 1,
    /// Factory not initialized
    NotInitialized = 2,
    /// Only admin can perform this action
    Unauthorized = 3,
    /// Market deployment failed
    DeploymentFailed = 4,
}

#[derive(Clone)]
#[contracttype]
pub enum DataKey {
    /// Admin address (can deploy markets)
    Admin,
    /// WASM hash of the LMSR market contract
    MarketWasmHash,
    /// List of deployed market addresses
    Markets,
    /// Default collateral token
    DefaultCollateralToken,
}

/// Market Factory Contract
///
/// Deploys and tracks LMSR prediction market contracts.
/// Each market is a separate contract instance.
#[contract]
pub struct MarketFactory;

#[contractimpl]
impl MarketFactory {
    /// Initialize the factory.
    ///
    /// # Arguments
    /// * `admin` - Address that can deploy markets
    /// * `market_wasm_hash` - WASM hash of the lmsr_market contract
    /// * `default_collateral_token` - Default collateral token for markets
    pub fn initialize(
        env: Env,
        admin: Address,
        market_wasm_hash: BytesN<32>,
        default_collateral_token: Address,
    ) -> Result<(), FactoryError> {
        if env.storage().instance().has(&DataKey::Admin) {
            return Err(FactoryError::AlreadyInitialized);
        }

        admin.require_auth();

        env.storage().instance().set(&DataKey::Admin, &admin);
        env.storage()
            .instance()
            .set(&DataKey::MarketWasmHash, &market_wasm_hash);
        env.storage()
            .instance()
            .set(&DataKey::DefaultCollateralToken, &default_collateral_token);
        env.storage()
            .instance()
            .set(&DataKey::Markets, &Vec::<Address>::new(&env));

        Ok(())
    }

    /// Deploy a new prediction market.
    ///
    /// # Arguments
    /// * `oracle` - Address that can resolve the market
    /// * `liquidity_param` - LMSR b parameter (scaled by 10^7)
    /// * `metadata_hash` - IPFS hash for market metadata
    /// * `initial_funding` - Collateral to fund the market
    /// * `salt` - Unique salt for deterministic address generation
    ///
    /// # Returns
    /// Address of the deployed market contract
    pub fn deploy_market(
        env: Env,
        oracle: Address,
        liquidity_param: i128,
        metadata_hash: String,
        initial_funding: i128,
        salt: BytesN<32>,
    ) -> Result<Address, FactoryError> {
        Self::require_initialized(&env)?;

        oracle.require_auth();

        let wasm_hash: BytesN<32> = env
            .storage()
            .instance()
            .get(&DataKey::MarketWasmHash)
            .unwrap();

        let collateral_token: Address = env
            .storage()
            .instance()
            .get(&DataKey::DefaultCollateralToken)
            .unwrap();

        // Deploy the market contract
        let market_address = env.deployer().with_current_contract(salt).deploy_v2(
            wasm_hash,
            (
                oracle.clone(),
                collateral_token,
                liquidity_param,
                metadata_hash,
                initial_funding,
            ),
        );

        // Track the deployed market
        let mut markets: Vec<Address> = env.storage().instance().get(&DataKey::Markets).unwrap();
        markets.push_back(market_address.clone());
        env.storage().instance().set(&DataKey::Markets, &markets);

        Ok(market_address)
    }

    /// Get all deployed market addresses.
    pub fn list_markets(env: Env) -> Result<Vec<Address>, FactoryError> {
        Self::require_initialized(&env)?;
        Ok(env.storage().instance().get(&DataKey::Markets).unwrap())
    }

    /// Get the number of deployed markets.
    pub fn market_count(env: Env) -> Result<u32, FactoryError> {
        Self::require_initialized(&env)?;
        let markets: Vec<Address> = env.storage().instance().get(&DataKey::Markets).unwrap();
        Ok(markets.len())
    }

    /// Get a market address by index.
    pub fn get_market(env: Env, index: u32) -> Result<Address, FactoryError> {
        Self::require_initialized(&env)?;
        let markets: Vec<Address> = env.storage().instance().get(&DataKey::Markets).unwrap();
        Ok(markets.get(index).unwrap())
    }

    /// Get the admin address.
    pub fn get_admin(env: Env) -> Result<Address, FactoryError> {
        Self::require_initialized(&env)?;
        Ok(env.storage().instance().get(&DataKey::Admin).unwrap())
    }

    /// Get the market WASM hash.
    pub fn get_market_wasm_hash(env: Env) -> Result<BytesN<32>, FactoryError> {
        Self::require_initialized(&env)?;
        Ok(env
            .storage()
            .instance()
            .get(&DataKey::MarketWasmHash)
            .unwrap())
    }

    /// Update the market WASM hash (admin only).
    pub fn set_market_wasm_hash(
        env: Env,
        admin: Address,
        new_wasm_hash: BytesN<32>,
    ) -> Result<(), FactoryError> {
        Self::require_initialized(&env)?;
        Self::require_admin(&env, &admin)?;

        admin.require_auth();

        env.storage()
            .instance()
            .set(&DataKey::MarketWasmHash, &new_wasm_hash);

        Ok(())
    }

    /// Update the default collateral token (admin only).
    pub fn set_default_collateral_token(
        env: Env,
        admin: Address,
        new_token: Address,
    ) -> Result<(), FactoryError> {
        Self::require_initialized(&env)?;
        Self::require_admin(&env, &admin)?;

        admin.require_auth();

        env.storage()
            .instance()
            .set(&DataKey::DefaultCollateralToken, &new_token);

        Ok(())
    }

    // --- Internal helpers ---

    fn require_initialized(env: &Env) -> Result<(), FactoryError> {
        if !env.storage().instance().has(&DataKey::Admin) {
            return Err(FactoryError::NotInitialized);
        }
        Ok(())
    }

    fn require_admin(env: &Env, caller: &Address) -> Result<(), FactoryError> {
        let admin: Address = env.storage().instance().get(&DataKey::Admin).unwrap();
        if *caller != admin {
            return Err(FactoryError::Unauthorized);
        }
        Ok(())
    }
}

#[cfg(test)]
mod test {
    use super::*;
    use soroban_sdk::{testutils::Address as _, Env};

    #[test]
    fn test_initialize() {
        let env = Env::default();
        env.mock_all_auths();

        let contract_id = env.register(MarketFactory, ());
        let client = MarketFactoryClient::new(&env, &contract_id);

        let admin = Address::generate(&env);
        let wasm_hash = BytesN::from_array(&env, &[0u8; 32]);
        let collateral_token = Address::generate(&env);

        client.initialize(&admin, &wasm_hash, &collateral_token);

        assert_eq!(client.get_admin(), admin);
        assert_eq!(client.get_market_wasm_hash(), wasm_hash);
        assert_eq!(client.market_count(), 0);
    }

    #[test]
    #[should_panic(expected = "AlreadyInitialized")]
    fn test_double_initialize() {
        let env = Env::default();
        env.mock_all_auths();

        let contract_id = env.register(MarketFactory, ());
        let client = MarketFactoryClient::new(&env, &contract_id);

        let admin = Address::generate(&env);
        let wasm_hash = BytesN::from_array(&env, &[0u8; 32]);
        let collateral_token = Address::generate(&env);

        client.initialize(&admin, &wasm_hash, &collateral_token);
        client.initialize(&admin, &wasm_hash, &collateral_token);
    }
}
