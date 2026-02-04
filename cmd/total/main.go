package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mtlprog/total/internal/config"
	"github.com/mtlprog/total/internal/handler"
	"github.com/mtlprog/total/internal/ipfs"
	"github.com/mtlprog/total/internal/logger"
	"github.com/mtlprog/total/internal/service"
	"github.com/mtlprog/total/internal/soroban"
	"github.com/mtlprog/total/internal/stellar"
	"github.com/mtlprog/total/internal/template"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Parse configuration from environment
	cfg := parseConfig()

	// Setup logging
	logger.Setup(logger.ParseLevel(cfg.LogLevel))

	// Log configuration
	slog.Info("configuration loaded",
		"network", cfg.Network,
		"horizon", cfg.NetworkConfig.HorizonURL,
		"soroban_rpc", cfg.NetworkConfig.SorobanRPCURL,
		"oracle", cfg.OraclePublicKey,
		"factory", cfg.FactoryContract,
	)

	// Initialize Stellar client
	stellarClient, err := stellar.NewHorizonClient(
		cfg.NetworkConfig.HorizonURL,
		cfg.NetworkConfig.NetworkPassphrase,
	)
	if err != nil {
		return fmt.Errorf("failed to create Stellar client: %w", err)
	}

	// Initialize Soroban client
	sorobanClient := soroban.NewClient(cfg.NetworkConfig.SorobanRPCURL)

	// Initialize transaction builder
	txBuilder := stellar.NewBuilder(
		stellarClient,
		cfg.NetworkConfig.NetworkPassphrase,
		config.DefaultBaseFee,
		sorobanClient,
	)

	// Initialize market service
	marketService := service.NewMarketService(
		stellarClient,
		sorobanClient,
		txBuilder,
		cfg.OraclePublicKey,
		slog.Default(),
	)

	// Initialize factory service (optional)
	var factoryService *service.FactoryService
	if cfg.FactoryContract != "" {
		factoryService = service.NewFactoryService(
			sorobanClient,
			stellarClient,
			txBuilder,
			cfg.FactoryContract,
			cfg.OraclePublicKey,
			slog.Default(),
		)
		slog.Info("factory service enabled", "contract", cfg.FactoryContract)
	} else {
		slog.Warn("MARKET_FACTORY_CONTRACT not set, market listing disabled")
	}

	// Initialize IPFS client
	ipfsClient := ipfs.NewClient(cfg.PinataAPIKey, cfg.PinataAPISecret)
	if cfg.PinataAPIKey != "" && cfg.PinataAPISecret != "" {
		slog.Info("IPFS client enabled with Pinata (read+write)")
	} else {
		slog.Info("IPFS client enabled (read-only)")
	}

	// Warmup IPFS cache
	if factoryService != nil {
		go warmupIPFSCache(factoryService, ipfsClient)
	}

	// Initialize templates
	tmpl, err := template.New()
	if err != nil {
		return fmt.Errorf("failed to load templates: %w", err)
	}

	// Initialize handler
	marketHandler := handler.NewMarketHandler(
		marketService,
		factoryService,
		ipfsClient,
		tmpl,
		cfg.OraclePublicKey,
		cfg.NetworkConfig.NetworkPassphrase,
		slog.Default(),
	)

	// Setup HTTP server
	mux := http.NewServeMux()
	marketHandler.RegisterRoutes(mux)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("starting server", "addr", "http://localhost:"+cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for shutdown signal
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case <-done:
		slog.Info("shutting down server")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	slog.Info("server stopped")
	return nil
}

// appConfig holds all application configuration.
type appConfig struct {
	Port            string
	LogLevel        string
	Network         string
	NetworkConfig   config.NetworkConfig
	OraclePublicKey string
	FactoryContract string
	PinataAPIKey    string
	PinataAPISecret string
}

// parseConfig reads configuration from environment variables.
func parseConfig() appConfig {
	network := strings.ToLower(getEnv("NETWORK", "testnet"))

	return appConfig{
		Port:            getEnv("PORT", config.DefaultPort),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		Network:         network,
		NetworkConfig:   config.GetNetworkConfig(network),
		OraclePublicKey: getEnv("ORACLE_PUBLIC_KEY", ""),
		FactoryContract: getEnv("MARKET_FACTORY_CONTRACT", ""),
		PinataAPIKey:    getEnv("PINATA_API_KEY", ""),
		PinataAPISecret: getEnv("PINATA_API_SECRET", ""),
	}
}

// getEnv returns environment variable value or default.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// warmupIPFSCache pre-fetches market metadata into cache.
func warmupIPFSCache(factoryService *service.FactoryService, ipfsClient *ipfs.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	markets, err := factoryService.ListMarkets(ctx)
	if err != nil {
		slog.Warn("failed to list markets for cache warmup", "error", err)
		return
	}

	if len(markets) == 0 {
		return
	}

	states, err := factoryService.GetMarketStates(ctx, markets)
	if err != nil {
		slog.Warn("failed to get market states for cache warmup", "error", err)
		return
	}

	var hashes []string
	for _, s := range states {
		if s.MetadataHash != "" {
			hashes = append(hashes, s.MetadataHash)
		}
	}

	if len(hashes) > 0 {
		slog.Info("warming up IPFS cache", "count", len(hashes))
		ipfsClient.Warmup(hashes)
	}
}
