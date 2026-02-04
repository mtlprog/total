package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/mtlprog/total/internal/config"
	"github.com/mtlprog/total/internal/handler"
	"github.com/mtlprog/total/internal/ipfs"
	"github.com/mtlprog/total/internal/logger"
	"github.com/mtlprog/total/internal/service"
	"github.com/mtlprog/total/internal/soroban"
	"github.com/mtlprog/total/internal/stellar"
	"github.com/mtlprog/total/internal/template"
	"github.com/urfave/cli/v2"
)

func main() {
	// Load .env file if present (ignore "file not found" errors, warn on other errors)
	if err := godotenv.Load(); err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to load .env file", "error", err)
		}
	}

	app := &cli.App{
		Name:  "total",
		Usage: "Stellar prediction market platform",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "log-level",
				Aliases: []string{"l"},
				Value:   "info",
				Usage:   "Log level (debug, info, warn, error)",
				EnvVars: []string{"LOG_LEVEL"},
			},
		},
		Before: func(c *cli.Context) error {
			logger.Setup(logger.ParseLevel(c.String("log-level")))
			return nil
		},
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "Start the web server",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "port",
						Aliases: []string{"p"},
						Value:   config.DefaultPort,
						Usage:   "HTTP server port",
						EnvVars: []string{"PORT"},
					},
					&cli.StringFlag{
						Name:    "horizon-url",
						Value:   config.DefaultHorizonURL,
						Usage:   "Stellar Horizon API URL",
						EnvVars: []string{"HORIZON_URL"},
					},
					&cli.StringFlag{
						Name:    "network-passphrase",
						Value:   config.DefaultNetworkPassphrase,
						Usage:   "Stellar network passphrase",
						EnvVars: []string{"NETWORK_PASSPHRASE"},
					},
					&cli.StringFlag{
						Name:     "oracle-public-key",
						Usage:    "Oracle public key (Stellar account that creates/resolves markets)",
						EnvVars:  []string{"ORACLE_PUBLIC_KEY"},
						Required: true,
					},
					&cli.StringFlag{
						Name:     "soroban-rpc-url",
						Value:    config.DefaultSorobanRPCURL,
						Usage:    "Soroban RPC URL",
						EnvVars:  []string{"SOROBAN_RPC_URL"},
						Required: true,
					},
					&cli.StringFlag{
						Name:    "market-factory-contract",
						Usage:   "Market factory contract ID (C...)",
						EnvVars: []string{"MARKET_FACTORY_CONTRACT"},
					},
					&cli.StringFlag{
						Name:    "pinata-api-key",
						Usage:   "Pinata API key for IPFS",
						EnvVars: []string{"PINATA_API_KEY"},
					},
					&cli.StringFlag{
						Name:    "pinata-api-secret",
						Usage:   "Pinata API secret for IPFS",
						EnvVars: []string{"PINATA_API_SECRET"},
					},
				},
				Action: runServe,
			},
		},
		Action: runServe,
	}

	if err := app.Run(os.Args); err != nil {
		slog.Error("application error", "error", err)
		os.Exit(1)
	}
}

func runServe(c *cli.Context) error {
	port := c.String("port")
	if port == "" {
		port = config.DefaultPort
	}

	horizonURL := c.String("horizon-url")
	networkPassphrase := c.String("network-passphrase")
	oraclePublicKey := c.String("oracle-public-key")
	sorobanRPCURL := c.String("soroban-rpc-url")
	factoryContract := c.String("market-factory-contract")
	pinataAPIKey := c.String("pinata-api-key")
	pinataAPISecret := c.String("pinata-api-secret")

	// Initialize Stellar client (for account lookups)
	stellarClient, err := stellar.NewHorizonClient(horizonURL, networkPassphrase)
	if err != nil {
		return fmt.Errorf("failed to create Stellar client: %w", err)
	}

	// Initialize Soroban client
	sorobanClient := soroban.NewClient(sorobanRPCURL)

	// Initialize transaction builder
	txBuilder := stellar.NewBuilder(stellarClient, networkPassphrase, config.DefaultBaseFee, sorobanClient)

	// Initialize market service
	marketService := service.NewMarketService(
		stellarClient,
		sorobanClient,
		txBuilder,
		oraclePublicKey,
		slog.Default(),
	)

	// Initialize factory service (optional, only if factory contract is configured)
	var factoryService *service.FactoryService
	if factoryContract != "" {
		factoryService = service.NewFactoryService(
			sorobanClient,
			stellarClient,
			txBuilder,
			factoryContract,
			oraclePublicKey,
			slog.Default(),
		)
		slog.Info("factory service enabled", "contract", factoryContract)
	} else {
		slog.Warn("factory contract not configured, market listing disabled")
	}

	// Initialize IPFS client (always enabled for reading, Pinata keys optional for writing)
	ipfsClient := ipfs.NewClient(pinataAPIKey, pinataAPISecret)
	if pinataAPIKey != "" && pinataAPISecret != "" {
		slog.Info("IPFS client enabled with Pinata (read+write)")
	} else {
		slog.Info("IPFS client enabled (read-only, no Pinata credentials)")
	}

	// Warmup IPFS cache with existing market metadata
	if factoryService != nil {
		go func() {
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
		}()
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
		oraclePublicKey,
		networkPassphrase,
		slog.Default(),
	)

	// Register routes
	mux := http.NewServeMux()
	marketHandler.RegisterRoutes(mux)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErr := make(chan error, 1)
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("starting server",
			"addr", "http://localhost:"+port,
			"horizon", horizonURL,
			"soroban_rpc", sorobanRPCURL,
			"oracle", oraclePublicKey,
			"factory", factoryContract,
		)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

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
