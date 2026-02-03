package config

const (
	DefaultPort = "8080"

	// Stellar configuration
	DefaultHorizonURL        = "https://horizon.stellar.org"
	DefaultNetworkPassphrase = "Public Global Stellar Network ; September 2015"
	DefaultBaseFee           = 100 // stroops

	// Soroban RPC configuration
	DefaultSorobanRPCURL     = "https://soroban-rpc.stellar.org:443"
	TestnetSorobanRPCURL     = "https://soroban-testnet.stellar.org:443"
	TestnetNetworkPassphrase = "Test SDF Network ; September 2015"

	// EURMTL issuer on mainnet
	EURMTLIssuer = "GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V"
	EURMTLCode   = "EURMTL"

	// EURMTL Stellar Asset Contract (SAC) address
	// This is the contract address for EURMTL token on Soroban
	// Deploy using: stellar contract asset deploy --asset EURMTL:GACKTN5...
	// Set via environment variable until deployed
	EURMTLContractID = ""

	// IPFS configuration
	DefaultIPFSGateway = "https://gateway.pinata.cloud/ipfs/"
	PinataAPIURL       = "https://api.pinata.cloud/pinning/pinJSONToIPFS"

	// Market configuration
	DefaultLiquidityParam   = 100.0
	InitialTokenSupply      = 1000000.0 // Initial supply of YES/NO tokens
	MaxAssetCodeLength      = 12
	MarketAccountMinReserve = 1.5 // XLM needed for market account
)
