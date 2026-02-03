package config

const (
	DefaultPort = "8080"

	// Stellar configuration
	DefaultHorizonURL        = "https://horizon.stellar.org"
	DefaultNetworkPassphrase = "Public Global Stellar Network ; September 2015"
	DefaultBaseFee           = 100 // stroops

	// EURMTL issuer on mainnet
	EURMTLIssuer = "GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V"
	EURMTLCode   = "EURMTL"

	// IPFS configuration
	DefaultIPFSGateway = "https://gateway.pinata.cloud/ipfs/"
	PinataAPIURL       = "https://api.pinata.cloud/pinning/pinJSONToIPFS"

	// Market configuration
	DefaultLiquidityParam   = 100.0
	InitialTokenSupply      = 1000000.0 // Initial supply of YES/NO tokens
	MaxAssetCodeLength      = 12
	MarketAccountMinReserve = 1.5 // XLM needed for market account
)
