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

	"github.com/mtlprog/total/internal/config"
	"github.com/mtlprog/total/internal/handler"
	"github.com/mtlprog/total/internal/logger"
	"github.com/mtlprog/total/internal/service"
	"github.com/mtlprog/total/internal/soroban"
	"github.com/mtlprog/total/internal/stellar"
	"github.com/mtlprog/total/internal/template"
	"github.com/urfave/cli/v2"
)

func main() {
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
					&cli.StringSliceFlag{
						Name:    "contract-ids",
						Usage:   "Known market contract IDs to track (comma-separated)",
						EnvVars: []string{"CONTRACT_IDS"},
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
	contractIDs := c.StringSlice("contract-ids")

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

	// Initialize templates
	tmpl, err := template.New()
	if err != nil {
		return fmt.Errorf("failed to load templates: %w", err)
	}

	// Initialize handler
	marketHandler := handler.NewMarketHandler(
		marketService,
		tmpl,
		oraclePublicKey,
		slog.Default(),
	)

	// Set known contract IDs
	if len(contractIDs) > 0 {
		marketHandler.SetMarketIDs(contractIDs)
	}

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
