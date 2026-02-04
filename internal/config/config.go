package config

const (
	DefaultPort = "8080"

	// Mainnet configuration
	MainnetHorizonURL        = "https://horizon.stellar.org"
	MainnetSorobanRPCURL     = "https://soroban-rpc.stellar.org:443"
	MainnetNetworkPassphrase = "Public Global Stellar Network ; September 2015"

	// Testnet configuration
	TestnetHorizonURL        = "https://horizon-testnet.stellar.org"
	TestnetSorobanRPCURL     = "https://soroban-testnet.stellar.org:443"
	TestnetNetworkPassphrase = "Test SDF Network ; September 2015"

	// Default base fee in stroops
	DefaultBaseFee = 100

	// IPFS configuration
	DefaultIPFSGateway = "https://gateway.pinata.cloud/ipfs/"
	PinataAPIURL       = "https://api.pinata.cloud/pinning/pinJSONToIPFS"

	// Market configuration
	DefaultLiquidityParam = 100.0
)

// NetworkConfig holds all network-specific configuration.
type NetworkConfig struct {
	HorizonURL        string
	SorobanRPCURL     string
	NetworkPassphrase string
}

// GetNetworkConfig returns configuration for the specified network.
// Defaults to testnet if network is not "mainnet".
func GetNetworkConfig(network string) NetworkConfig {
	if network == "mainnet" {
		return NetworkConfig{
			HorizonURL:        MainnetHorizonURL,
			SorobanRPCURL:     MainnetSorobanRPCURL,
			NetworkPassphrase: MainnetNetworkPassphrase,
		}
	}
	// Default to testnet
	return NetworkConfig{
		HorizonURL:        TestnetHorizonURL,
		SorobanRPCURL:     TestnetSorobanRPCURL,
		NetworkPassphrase: TestnetNetworkPassphrase,
	}
}
