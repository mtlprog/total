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
	"github.com/mtlprog/total/internal/ipfs"
	"github.com/mtlprog/total/internal/logger"
	"github.com/mtlprog/total/internal/service"
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
						Name:    "oracle-secret-key",
						Usage:   "Oracle secret key for signing market transactions (S...)",
						EnvVars: []string{"ORACLE_SECRET_KEY"},
					},
					&cli.StringFlag{
						Name:    "pinata-api-key",
						Usage:   "Pinata API key for IPFS",
						EnvVars: []string{"PINATA_API_KEY"},
					},
					&cli.StringFlag{
						Name:    "pinata-secret",
						Usage:   "Pinata API secret for IPFS",
						EnvVars: []string{"PINATA_SECRET"},
					},
					&cli.StringSliceFlag{
						Name:    "market-ids",
						Usage:   "Known market IDs to track (comma-separated)",
						EnvVars: []string{"MARKET_IDS"},
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
	oracleSecretKey := c.String("oracle-secret-key")
	pinataAPIKey := c.String("pinata-api-key")
	pinataSecret := c.String("pinata-secret")
	marketIDs := c.StringSlice("market-ids")

	// Initialize Stellar client
	stellarClient := stellar.NewHorizonClient(horizonURL, networkPassphrase)

	// Initialize transaction builder
	txBuilder, err := stellar.NewBuilder(stellarClient, networkPassphrase, config.DefaultBaseFee, oracleSecretKey)
	if err != nil {
		return fmt.Errorf("failed to create transaction builder: %w", err)
	}

	// Initialize IPFS client
	ipfsClient := ipfs.NewClient(pinataAPIKey, pinataSecret)

	// Initialize market service
	marketService := service.NewMarketService(
		stellarClient,
		txBuilder,
		ipfsClient,
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

	// Set known market IDs
	if len(marketIDs) > 0 {
		marketHandler.SetMarketIDs(marketIDs)
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
