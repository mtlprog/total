package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mtlprog/total/internal/chart"
	"github.com/mtlprog/total/internal/ipfs"
	"github.com/mtlprog/total/internal/lmsr"
	"github.com/mtlprog/total/internal/model"
	"github.com/mtlprog/total/internal/service"
	"github.com/mtlprog/total/internal/soroban"
	"github.com/mtlprog/total/internal/stellar"
	"github.com/mtlprog/total/internal/template"
	"github.com/stellar/go-stellar-sdk/keypair"
)

// MarketHandler handles HTTP requests for prediction markets.
type MarketHandler struct {
	marketService     *service.MarketService
	factoryService    *service.FactoryService
	eventService      *service.EventService
	ipfsClient        *ipfs.Client
	tmpl              *template.Template
	oraclePublicKey   string
	networkPassphrase string
	logger            *slog.Logger
}

// NewMarketHandler creates a new market handler.
func NewMarketHandler(
	marketService *service.MarketService,
	factoryService *service.FactoryService,
	eventService *service.EventService,
	ipfsClient *ipfs.Client,
	tmpl *template.Template,
	oraclePublicKey string,
	networkPassphrase string,
	logger *slog.Logger,
) *MarketHandler {
	return &MarketHandler{
		marketService:     marketService,
		factoryService:    factoryService,
		eventService:      eventService,
		ipfsClient:        ipfsClient,
		tmpl:              tmpl,
		oraclePublicKey:   oraclePublicKey,
		networkPassphrase: networkPassphrase,
		logger:            logger,
	}
}

const cookieMaxAge = 10 * 365 * 24 * 3600 // 10 years

// accountIDFromCookie reads the account_id cookie value.
func accountIDFromCookie(r *http.Request) string {
	c, err := r.Cookie("account_id")
	if err != nil || c.Value == "" {
		return ""
	}
	// Basic validation: must look like a Stellar public key
	if _, err := keypair.ParseAddress(c.Value); err != nil {
		return ""
	}
	return c.Value
}

// setAccountIDCookie writes the account_id cookie.
func setAccountIDCookie(w http.ResponseWriter, accountID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "account_id",
		Value:    accountID,
		Path:     "/",
		MaxAge:   cookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// RegisterRoutes registers market routes.
func (h *MarketHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /", h.handleListMarkets)
	mux.HandleFunc("GET /markets", h.handleListMarkets)
	mux.HandleFunc("GET /market/{id}", h.handleMarketDetail)
	mux.HandleFunc("POST /market/{id}/quote", h.handleGetQuote)
	mux.HandleFunc("POST /market/{id}/buy", h.handleBuildBuyTx)
	mux.HandleFunc("POST /market/{id}/sell", h.handleBuildSellTx)
	mux.HandleFunc("POST /market/{id}/resolve", h.handleResolveMarket)
	mux.HandleFunc("POST /market/{id}/claim", h.handleBuildClaimTx)
	mux.HandleFunc("POST /market/{id}/withdraw", h.handleBuildWithdrawTx)
	mux.HandleFunc("GET /market/{id}/yes", h.handleOutcomePage)
	mux.HandleFunc("GET /market/{id}/no", h.handleOutcomePage)
	mux.HandleFunc("POST /account", h.handleSetAccount)
	mux.HandleFunc("GET /oracle", h.handleOracleAdmin)
	mux.HandleFunc("GET /deploy", h.handleRedirectToOracle)
	mux.HandleFunc("POST /deploy", h.handleBuildDeployTx)
	mux.HandleFunc("GET /health", h.handleHealth)
	mux.HandleFunc("POST /api/quote/{id}", h.handleAPIQuote)
	mux.HandleFunc("POST /api/mtl-wallet", h.handleMTLWallet)
}

// networkName returns "testnet" or "public" based on the network passphrase.
func (h *MarketHandler) networkName() string {
	if strings.Contains(h.networkPassphrase, "Test") {
		return "testnet"
	}
	return "public"
}

// MarketView represents a market for display in templates.
type MarketView struct {
	ID             string
	Question       string
	Description    string
	PriceYes       float64
	PriceNo        float64
	YesSold        float64
	NoSold         float64
	IsResolved     bool
	Resolution     string
	LiquidityParam float64
	MetadataHash   string
	MetadataError  string // Non-empty when IPFS metadata failed to load
}

// shortID formats an ID as "first8...last8" for display.
// IDs 19 characters or shorter are returned unchanged.
func shortID(id string) string {
	if len(id) <= 19 {
		return id
	}
	return id[:8] + "..." + id[len(id)-8:]
}

// handleListMarkets renders the list of all markets from factory.
func (h *MarketHandler) handleListMarkets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	accountID := accountIDFromCookie(r)

	if h.factoryService == nil || !h.factoryService.HasFactory() {
		data := map[string]any{
			"Markets":         []MarketView{},
			"OraclePublicKey": h.oraclePublicKey,
			"Error":           "Factory contract not configured",
			"ActiveNav":       "markets",
			"Network":         h.networkName(),
			"AccountID":       accountID,
		}
		if err := h.tmpl.Render(w, "markets", data); err != nil {
			h.logger.Error("failed to render template", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	// Get all market IDs from factory
	contractIDs, err := h.factoryService.ListMarkets(ctx)
	if err != nil {
		h.logger.Error("failed to list markets", "error", err)
		data := map[string]any{
			"Markets":         []MarketView{},
			"OraclePublicKey": h.oraclePublicKey,
			"Error":           "Failed to fetch markets from factory",
			"ActiveNav":       "markets",
			"Network":         h.networkName(),
			"AccountID":       accountID,
		}
		if err := h.tmpl.Render(w, "markets", data); err != nil {
			h.logger.Error("failed to render template", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	// Get states for all markets
	states, err := h.factoryService.GetMarketStates(ctx, contractIDs)
	if err != nil {
		h.logger.Warn("failed to get some market states", "error", err)
	}

	// Convert states to views with metadata from IPFS
	markets := h.buildMarketViews(ctx, states)

	data := map[string]any{
		"Markets":         markets,
		"OraclePublicKey": h.oraclePublicKey,
		"ActiveNav":       "markets",
		"Network":         h.networkName(),
		"AccountID":       accountID,
	}

	if err := h.tmpl.Render(w, "markets", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// buildMarketViews converts market states to views, fetching metadata in parallel.
// Blocks until all metadata fetches complete.
func (h *MarketHandler) buildMarketViews(ctx context.Context, states []service.MarketState) []MarketView {
	views := make([]MarketView, len(states))
	var wg sync.WaitGroup

	for i, state := range states {
		wg.Add(1)
		go func(idx int, s service.MarketState) {
			defer wg.Done()

			view := MarketView{
				ID:           s.ContractID,
				PriceYes:     s.PriceYes,
				PriceNo:      s.PriceNo,
				YesSold:      float64(s.YesSold) / float64(soroban.ScaleFactor),
				NoSold:       float64(s.NoSold) / float64(soroban.ScaleFactor),
				IsResolved:   s.Resolved,
				MetadataHash: s.MetadataHash,
			}

			// Fetch metadata from IPFS
			if s.MetadataHash != "" && h.ipfsClient != nil {
				var metadata model.MarketMetadata
				if err := h.ipfsClient.GetJSON(ctx, s.MetadataHash, &metadata); err != nil {
					h.logger.Warn("failed to fetch metadata", "hash", s.MetadataHash, "error", err)
					view.Question = "Market " + shortID(s.ContractID)
					view.MetadataError = "Failed to load market details from IPFS"
				} else {
					view.Question = metadata.Question
					view.Description = metadata.Description
				}
			} else {
				view.Question = "Market " + shortID(s.ContractID)
			}

			views[idx] = view
		}(i, state)
	}

	wg.Wait()
	return views
}

// handleMarketDetail renders a single market's detail page.
func (h *MarketHandler) handleMarketDetail(w http.ResponseWriter, r *http.Request) {
	contractID := r.PathValue("id")
	if contractID == "" {
		http.Error(w, "Contract ID required", http.StatusBadRequest)
		return
	}

	if h.factoryService == nil || !h.factoryService.HasFactory() {
		http.Error(w, "Factory contract not configured", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()

	// Get market state
	states, err := h.factoryService.GetMarketStates(ctx, []string{contractID})
	if err != nil {
		h.logger.Error("failed to get market state", "contract_id", contractID, "error", err)
		h.writeError(w, r, err, "contract_id", contractID)
		return
	}
	if len(states) == 0 {
		http.Error(w, "Market not found", http.StatusNotFound)
		return
	}

	state := states[0]

	market := model.Market{
		ID:       state.ContractID,
		YesSold:  float64(state.YesSold) / float64(soroban.ScaleFactor),
		NoSold:   float64(state.NoSold) / float64(soroban.ScaleFactor),
		PriceYes: state.PriceYes,
		PriceNo:  state.PriceNo,
	}

	if state.Resolved && state.WinningOutcome != "" {
		market.Resolution = model.Outcome(state.WinningOutcome)
	}

	// Fetch metadata from IPFS
	if state.MetadataHash != "" && h.ipfsClient != nil {
		var metadata model.MarketMetadata
		if err := h.ipfsClient.GetJSON(ctx, state.MetadataHash, &metadata); err != nil {
			h.logger.Warn("failed to fetch metadata", "hash", state.MetadataHash, "error", err)
			market.Question = "Market " + shortID(contractID)
		} else {
			market.Question = metadata.Question
			market.Description = metadata.Description
			market.ResolutionSource = metadata.ResolutionSource
			market.Category = metadata.Category
			market.EndDate = metadata.EndDate
			market.CreatedAt = metadata.CreatedAt
		}
		market.MetadataHash = state.MetadataHash
	} else {
		market.Question = "Market " + shortID(contractID)
	}

	// Resolve account: cookie first, then query param override
	accountID := accountIDFromCookie(r)
	accountKey := strings.TrimSpace(r.URL.Query().Get("account"))
	if accountKey != "" {
		accountID = accountKey
	}

	// Fetch user balance if we have an account
	var userBalance *service.UserBalance
	var balanceError string
	if accountID != "" {
		if _, err := keypair.ParseAddress(accountID); err != nil {
			balanceError = "Invalid Stellar public key format."
		} else {
			balance, err := h.marketService.GetBalance(ctx, contractID, accountID)
			if err != nil {
				h.logger.Error("failed to get user balance", "account", accountID, "error", err)
				balanceError = "Failed to load balance — please try again."
			} else {
				userBalance = balance
			}
		}
	}

	// Fetch trade events and build price chart
	var tradeEvents []service.TradeEvent
	var priceChart string
	var eventsError string
	if h.eventService != nil {
		events, err := h.eventService.GetTradeEvents(ctx, contractID)
		if err != nil {
			h.logger.Warn("failed to get trade events", "contract_id", contractID, "error", err)
			eventsError = "Failed to load trade history."
		} else {
			tradeEvents = events
			if len(events) > 0 {
				points := eventsToChartPoints(events)
				priceChart = chart.RenderPriceChart(points, chart.DefaultWidth, chart.DefaultHeight)
			}
		}
	}

	data := map[string]any{
		"Market":          &market,
		"OraclePublicKey": h.oraclePublicKey,
		"PriceChart":      priceChart,
		"TradeEvents":     tradeEvents,
		"EventsError":     eventsError,
		"ActiveNav":       "markets",
		"Network":         h.networkName(),
		"UserBalance":     userBalance,
		"AccountID":       accountID,
		"BalanceError":    balanceError,
	}

	if err := h.tmpl.Render(w, "market", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleGetQuote returns a price quote for buying tokens.
func (h *MarketHandler) handleGetQuote(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	contractID := r.PathValue("id")
	outcomeStr := r.FormValue("outcome")
	amountStr := r.FormValue("amount")

	outcome, err := model.ParseOutcome(outcomeStr)
	if err != nil {
		http.Error(w, "Invalid outcome: must be YES or NO", http.StatusBadRequest)
		return
	}

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		http.Error(w, "Invalid amount", http.StatusBadRequest)
		return
	}

	quote, err := h.marketService.GetQuote(r.Context(), contractID, outcome, amount)
	if err != nil {
		h.writeError(w, r, err, "contract_id", contractID, "outcome", outcome, "amount", amount)
		return
	}

	// Return quote page
	data := map[string]any{
		"Quote":      quote,
		"ContractID": contractID,
		"ActiveNav":  "markets",
		"Network":    h.networkName(),
		"AccountID":  accountIDFromCookie(r),
	}

	if err := h.tmpl.Render(w, "quote", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleBuildBuyTx builds a transaction for buying tokens.
func (h *MarketHandler) handleBuildBuyTx(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	contractID := r.PathValue("id")
	userPubKey := strings.TrimSpace(r.FormValue("user_public_key"))
	outcomeStr := r.FormValue("outcome")
	amountStr := r.FormValue("amount")
	slippageStr := r.FormValue("slippage")

	// Validate public key using Stellar SDK
	if _, err := keypair.ParseAddress(userPubKey); err != nil {
		http.Error(w, "Invalid Stellar public key", http.StatusBadRequest)
		return
	}

	outcome, err := model.ParseOutcome(outcomeStr)
	if err != nil {
		http.Error(w, "Invalid outcome: must be YES or NO", http.StatusBadRequest)
		return
	}

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		http.Error(w, "Invalid amount", http.StatusBadRequest)
		return
	}

	// Parse slippage (default 1%, max 10%)
	slippage := model.DefaultSlippage
	if slippageStr != "" {
		s, err := strconv.ParseFloat(slippageStr, 64)
		if err != nil {
			http.Error(w, "Invalid slippage: must be a number", http.StatusBadRequest)
			return
		}
		if s <= 0 || s > model.MaxSlippage {
			http.Error(w, fmt.Sprintf("Invalid slippage: must be between 0 and %.0f%% (e.g., 0.01 for 1%%)", model.MaxSlippage*100), http.StatusBadRequest)
			return
		}
		slippage = s
	}

	req := service.BuyRequest{
		TradeRequest: service.TradeRequest{
			UserPublicKey: userPubKey,
			ContractID:    contractID,
			Outcome:       outcome,
			ShareAmount:   amount,
			Slippage:      slippage,
		},
	}

	result, err := h.marketService.BuildBuyTx(r.Context(), req)
	if err != nil {
		h.writeError(w, r, err, "contract_id", contractID, "outcome", outcome, "amount", amount)
		return
	}

	// Render XDR result page
	data := map[string]any{
		"Result":            result,
		"MarketID":          contractID,
		"ActiveNav":         "markets",
		"Network":           h.networkName(),
		"NetworkPassphrase": h.networkPassphrase,
		"AccountID":         accountIDFromCookie(r),
	}

	if err := h.tmpl.Render(w, "transaction", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleBuildSellTx builds a transaction for selling tokens.
func (h *MarketHandler) handleBuildSellTx(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	contractID := r.PathValue("id")
	userPubKey := strings.TrimSpace(r.FormValue("user_public_key"))
	outcomeStr := r.FormValue("outcome")
	amountStr := r.FormValue("amount")
	slippageStr := r.FormValue("slippage")

	// Validate public key using Stellar SDK
	if _, err := keypair.ParseAddress(userPubKey); err != nil {
		http.Error(w, "Invalid Stellar public key", http.StatusBadRequest)
		return
	}

	outcome, err := model.ParseOutcome(outcomeStr)
	if err != nil {
		http.Error(w, "Invalid outcome: must be YES or NO", http.StatusBadRequest)
		return
	}

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		http.Error(w, "Invalid amount", http.StatusBadRequest)
		return
	}

	// Parse slippage (default 1%, max 10%)
	slippage := model.DefaultSlippage
	if slippageStr != "" {
		s, err := strconv.ParseFloat(slippageStr, 64)
		if err != nil {
			http.Error(w, "Invalid slippage: must be a number", http.StatusBadRequest)
			return
		}
		if s <= 0 || s > model.MaxSlippage {
			http.Error(w, fmt.Sprintf("Invalid slippage: must be between 0 and %.0f%% (e.g., 0.01 for 1%%)", model.MaxSlippage*100), http.StatusBadRequest)
			return
		}
		slippage = s
	}

	req := service.SellRequest{
		TradeRequest: service.TradeRequest{
			UserPublicKey: userPubKey,
			ContractID:    contractID,
			Outcome:       outcome,
			ShareAmount:   amount,
			Slippage:      slippage,
		},
	}

	result, err := h.marketService.BuildSellTx(r.Context(), req)
	if err != nil {
		h.writeError(w, r, err, "contract_id", contractID, "outcome", outcome, "amount", amount)
		return
	}

	// Render XDR result page
	data := map[string]any{
		"Result":            result,
		"MarketID":          contractID,
		"ActiveNav":         "markets",
		"Network":           h.networkName(),
		"NetworkPassphrase": h.networkPassphrase,
		"AccountID":         accountIDFromCookie(r),
	}

	if err := h.tmpl.Render(w, "transaction", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleResolveMarket resolves a market.
func (h *MarketHandler) handleResolveMarket(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	contractID := r.PathValue("id")
	outcomeStr := r.FormValue("outcome")

	outcome, err := model.ParseOutcome(outcomeStr)
	if err != nil {
		http.Error(w, "Invalid outcome: must be YES or NO", http.StatusBadRequest)
		return
	}

	req := service.ResolveRequest{
		OraclePublicKey: h.oraclePublicKey,
		ContractID:      contractID,
		WinningOutcome:  outcome,
	}

	result, err := h.marketService.BuildResolveTx(r.Context(), req)
	if err != nil {
		h.writeError(w, r, err, "contract_id", contractID, "outcome", outcome)
		return
	}

	data := map[string]any{
		"Result":            result,
		"MarketID":          contractID,
		"ActiveNav":         "oracle",
		"Network":           h.networkName(),
		"NetworkPassphrase": h.networkPassphrase,
		"AccountID":         accountIDFromCookie(r),
	}

	if err := h.tmpl.Render(w, "transaction", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleBuildClaimTx builds a transaction to claim winnings.
func (h *MarketHandler) handleBuildClaimTx(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	contractID := r.PathValue("id")
	userPubKey := strings.TrimSpace(r.FormValue("user_public_key"))

	// Validate public key using Stellar SDK
	if _, err := keypair.ParseAddress(userPubKey); err != nil {
		http.Error(w, "Invalid Stellar public key", http.StatusBadRequest)
		return
	}

	req := service.ClaimRequest{
		UserPublicKey: userPubKey,
		ContractID:    contractID,
	}

	result, err := h.marketService.BuildClaimTx(r.Context(), req)
	if err != nil {
		h.writeError(w, r, err, "contract_id", contractID, "user_public_key", userPubKey)
		return
	}

	data := map[string]any{
		"Result":            result,
		"MarketID":          contractID,
		"ActiveNav":         "markets",
		"Network":           h.networkName(),
		"NetworkPassphrase": h.networkPassphrase,
		"AccountID":         accountIDFromCookie(r),
	}

	if err := h.tmpl.Render(w, "transaction", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleBuildWithdrawTx builds a transaction for oracle to withdraw remaining pool.
func (h *MarketHandler) handleBuildWithdrawTx(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	contractID := r.PathValue("id")
	oraclePubKey := strings.TrimSpace(r.FormValue("oracle_public_key"))

	// Validate public key using Stellar SDK
	if _, err := keypair.ParseAddress(oraclePubKey); err != nil {
		http.Error(w, "Invalid Stellar public key", http.StatusBadRequest)
		return
	}

	req := service.WithdrawRequest{
		OraclePublicKey: oraclePubKey,
		ContractID:      contractID,
	}

	result, err := h.marketService.BuildWithdrawTx(r.Context(), req)
	if err != nil {
		h.writeError(w, r, err, "contract_id", contractID, "oracle_public_key", oraclePubKey)
		return
	}

	data := map[string]any{
		"Result":            result,
		"MarketID":          contractID,
		"ActiveNav":         "oracle",
		"Network":           h.networkName(),
		"NetworkPassphrase": h.networkPassphrase,
		"AccountID":         accountIDFromCookie(r),
	}

	if err := h.tmpl.Render(w, "transaction", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleSetAccount handles POST /account to save account_id cookie.
func (h *MarketHandler) handleSetAccount(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	accountID := strings.TrimSpace(r.FormValue("account_id"))
	if accountID == "" {
		// Clear cookie
		http.SetCookie(w, &http.Cookie{
			Name:   "account_id",
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})
	} else {
		if _, err := keypair.ParseAddress(accountID); err != nil {
			http.Error(w, "Invalid Stellar public key", http.StatusBadRequest)
			return
		}
		setAccountIDCookie(w, accountID)
	}

	// Redirect back to referrer path (same-origin only) or home
	redirect := "/"
	if referer := r.Header.Get("Referer"); referer != "" {
		if u, err := url.Parse(referer); err == nil && u.Path != "" {
			redirect = u.Path
		}
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// handleOutcomePage renders a dedicated YES or NO page for social media sharing.
func (h *MarketHandler) handleOutcomePage(w http.ResponseWriter, r *http.Request) {
	contractID := r.PathValue("id")
	if contractID == "" {
		http.Error(w, "Contract ID required", http.StatusBadRequest)
		return
	}

	// Determine outcome from URL path
	path := r.URL.Path
	var outcome string
	if strings.HasSuffix(path, "/yes") {
		outcome = "YES"
	} else if strings.HasSuffix(path, "/no") {
		outcome = "NO"
	} else {
		http.Error(w, "Invalid outcome", http.StatusBadRequest)
		return
	}

	if h.factoryService == nil || !h.factoryService.HasFactory() {
		http.Error(w, "Factory contract not configured", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()

	states, err := h.factoryService.GetMarketStates(ctx, []string{contractID})
	if err != nil {
		h.writeError(w, r, err, "contract_id", contractID)
		return
	}
	if len(states) == 0 {
		http.Error(w, "Market not found", http.StatusNotFound)
		return
	}

	state := states[0]
	market := model.Market{
		ID:       state.ContractID,
		YesSold:  float64(state.YesSold) / float64(soroban.ScaleFactor),
		NoSold:   float64(state.NoSold) / float64(soroban.ScaleFactor),
		PriceYes: state.PriceYes,
		PriceNo:  state.PriceNo,
	}

	if state.Resolved && state.WinningOutcome != "" {
		market.Resolution = model.Outcome(state.WinningOutcome)
	}

	if state.MetadataHash != "" && h.ipfsClient != nil {
		var metadata model.MarketMetadata
		if err := h.ipfsClient.GetJSON(ctx, state.MetadataHash, &metadata); err != nil {
			h.logger.Warn("failed to fetch metadata", "hash", state.MetadataHash, "error", err)
			market.Question = "Market " + shortID(contractID)
		} else {
			market.Question = metadata.Question
			market.Description = metadata.Description
		}
		market.MetadataHash = state.MetadataHash
	} else {
		market.Question = "Market " + shortID(contractID)
	}

	// Get user balance from cookie
	accountID := accountIDFromCookie(r)
	var userBalance *service.UserBalance
	var balanceError string
	if accountID != "" {
		balance, err := h.marketService.GetBalance(ctx, contractID, accountID)
		if err != nil {
			h.logger.Warn("failed to get user balance for outcome page", "error", err)
			balanceError = "Failed to load balance — please try again."
		} else {
			userBalance = balance
		}
	}

	// Build OG meta tags
	var price float64
	if outcome == "YES" {
		price = market.PriceYes
	} else {
		price = market.PriceNo
	}
	ogTitle := fmt.Sprintf("Vote %s: %s", outcome, market.Question)
	ogDescription := fmt.Sprintf("%s is at %.0f%% — Trade on Total", outcome, price*100)

	data := map[string]any{
		"Market":            &market,
		"Outcome":           outcome,
		"OutcomePrice":      price,
		"OGTitle":           ogTitle,
		"OGDescription":     ogDescription,
		"UserBalance":       userBalance,
		"BalanceError":      balanceError,
		"AccountID":         accountID,
		"ActiveNav":         "markets",
		"Network":           h.networkName(),
		"NetworkPassphrase": h.networkPassphrase,
	}

	if err := h.tmpl.Render(w, "outcome", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// eventsToChartPoints converts trade events into price points for charting.
// NOTE: This is a ratio-based approximation, not true LMSR pricing (which requires
// the liquidity parameter b). See calculatePrices in factory.go for the same caveat.
func eventsToChartPoints(events []service.TradeEvent) []model.PricePoint {
	var yesSold, noSold float64
	points := make([]model.PricePoint, 0, len(events))

	for _, evt := range events {
		switch evt.Kind {
		case service.TradeKindBuy:
			if evt.Outcome == "YES" {
				yesSold += evt.Amount
			} else {
				noSold += evt.Amount
			}
		case service.TradeKindSell:
			if evt.Outcome == "YES" {
				yesSold -= evt.Amount
			} else {
				noSold -= evt.Amount
			}
		}

		// Clamp to non-negative (partial event windows may cause underflow)
		if yesSold < 0 {
			yesSold = 0
		}
		if noSold < 0 {
			noSold = 0
		}

		// Approximate YES price from ratio
		total := yesSold + noSold
		priceYes := 0.5
		if total > 0 {
			priceYes = yesSold / total
		}
		if priceYes < 0.01 {
			priceYes = 0.01
		}
		if priceYes > 0.99 {
			priceYes = 0.99
		}

		points = append(points, model.PricePoint{
			Timestamp: evt.Timestamp,
			PriceYes:  priceYes,
		})
	}

	return points
}

// handleRedirectToOracle redirects /deploy to /oracle.
func (h *MarketHandler) handleRedirectToOracle(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/oracle", http.StatusMovedPermanently)
}

// handleOracleAdmin renders the oracle admin page with deploy/resolve/withdraw forms.
func (h *MarketHandler) handleOracleAdmin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var markets []MarketView
	var factoryContract string
	var marketsError string

	if h.factoryService != nil && h.factoryService.HasFactory() {
		factoryContract = h.factoryService.FactoryContractID()

		// Get all markets for the dropdowns
		contractIDs, err := h.factoryService.ListMarkets(ctx)
		if err != nil {
			h.logger.Warn("failed to list markets for oracle admin", "error", err)
			marketsError = "Failed to load markets from factory"
		} else {
			states, err := h.factoryService.GetMarketStates(ctx, contractIDs)
			if err != nil {
				h.logger.Warn("failed to get market states for oracle admin", "error", err)
				marketsError = "Failed to load market states"
			} else {
				markets = h.buildMarketViews(ctx, states)
			}
		}
	}

	data := map[string]any{
		"OraclePublicKey":       h.oraclePublicKey,
		"DefaultLiquidityParam": 100.0,
		"FactoryContract":       factoryContract,
		"Markets":               markets,
		"MarketsError":          marketsError,
		"ActiveNav":             "oracle",
		"Network":               h.networkName(),
		"AccountID":             accountIDFromCookie(r),
	}

	if err := h.tmpl.Render(w, "oracle", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleBuildDeployTx builds a transaction to deploy a new market.
func (h *MarketHandler) handleBuildDeployTx(w http.ResponseWriter, r *http.Request) {
	if h.factoryService == nil || !h.factoryService.HasFactory() {
		http.Error(w, "Factory contract not configured", http.StatusServiceUnavailable)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	metadataHash := strings.TrimSpace(r.FormValue("metadata_hash"))
	liquidityParamStr := r.FormValue("liquidity_param")
	initialFundingStr := r.FormValue("initial_funding")

	if metadataHash == "" {
		http.Error(w, "Metadata hash is required (upload metadata to IPFS first)", http.StatusBadRequest)
		return
	}

	// Validate IPFS CID format to prevent SSRF
	if err := ipfs.ValidateCID(metadataHash); err != nil {
		http.Error(w, "Invalid IPFS hash format (must be CIDv0 Qm... or CIDv1 b...)", http.StatusBadRequest)
		return
	}

	liquidityParam, err := strconv.ParseFloat(liquidityParamStr, 64)
	if err != nil || liquidityParam <= 0 {
		http.Error(w, "Invalid liquidity parameter", http.StatusBadRequest)
		return
	}

	initialFunding, err := strconv.ParseFloat(initialFundingStr, 64)
	if err != nil || initialFunding <= 0 {
		http.Error(w, "Invalid initial funding", http.StatusBadRequest)
		return
	}

	req := service.DeployMarketRequest{
		LiquidityParam: liquidityParam,
		MetadataHash:   metadataHash,
		InitialFunding: initialFunding,
	}

	result, err := h.factoryService.BuildDeployMarketTx(r.Context(), req)
	if err != nil {
		h.writeError(w, r, err, "liquidity_param", liquidityParam, "metadata_hash", metadataHash)
		return
	}

	data := map[string]any{
		"Result":            result,
		"MarketID":          "new",
		"ActiveNav":         "oracle",
		"Network":           h.networkName(),
		"NetworkPassphrase": h.networkPassphrase,
		"AccountID":         accountIDFromCookie(r),
	}

	if err := h.tmpl.Render(w, "transaction", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleHealth returns health status.
func (h *MarketHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}

// errorResponse contains both message and status code for an error.
type errorResponse struct {
	Message string
	Status  int
}

// mapError maps internal errors to user-friendly messages and HTTP status codes.
// Uses errors.Is() to properly match wrapped errors.
func mapError(err error) errorResponse {
	switch {
	// Not found errors -> 404
	case errors.Is(err, service.ErrMarketNotFound):
		return errorResponse{"Market not found", http.StatusNotFound}

	// Business logic errors -> 409 Conflict
	case errors.Is(err, service.ErrMarketResolved):
		return errorResponse{"Market has already been resolved", http.StatusConflict}

	// Factory errors
	case errors.Is(err, service.ErrFactoryNotConfigured):
		return errorResponse{"Factory contract not configured", http.StatusServiceUnavailable}
	case errors.Is(err, service.ErrInvalidMetadataHash):
		return errorResponse{"Invalid metadata hash", http.StatusBadRequest}

	// Validation errors -> 400 Bad Request
	case errors.Is(err, service.ErrInvalidOutcome):
		return errorResponse{"Invalid outcome: must be YES or NO", http.StatusBadRequest}
	case errors.Is(err, model.ErrEmptyQuestion):
		return errorResponse{"Question is required", http.StatusBadRequest}
	case errors.Is(err, model.ErrQuestionTooLong):
		return errorResponse{fmt.Sprintf("Question exceeds maximum length (%d characters)", model.MaxQuestionLength), http.StatusBadRequest}
	case errors.Is(err, model.ErrDescriptionTooLong):
		return errorResponse{fmt.Sprintf("Description exceeds maximum length (%d characters)", model.MaxDescriptionLength), http.StatusBadRequest}
	case errors.Is(err, model.ErrInvalidLiquidityParam):
		return errorResponse{"Liquidity parameter must be a positive number", http.StatusBadRequest}
	case errors.Is(err, model.ErrInvalidShareAmount):
		return errorResponse{"Share amount must be a positive number", http.StatusBadRequest}
	case errors.Is(err, model.ErrCloseTimeInPast):
		return errorResponse{"Close time must be in the future", http.StatusBadRequest}
	case errors.Is(err, model.ErrInvalidPublicKey):
		return errorResponse{"Invalid Stellar public key format", http.StatusBadRequest}
	case errors.Is(err, model.ErrInvalidSlippage):
		return errorResponse{fmt.Sprintf("Slippage must be between 0 and %.0f%%", model.MaxSlippage*100), http.StatusBadRequest}

	// LMSR errors -> 400 Bad Request
	case errors.Is(err, lmsr.ErrInvalidOutcome):
		return errorResponse{"Invalid outcome: must be YES or NO", http.StatusBadRequest}
	case errors.Is(err, lmsr.ErrNegativeAmount):
		return errorResponse{"Amount must be positive", http.StatusBadRequest}
	case errors.Is(err, lmsr.ErrInsufficientTokens):
		return errorResponse{"Insufficient tokens available", http.StatusBadRequest}
	case errors.Is(err, lmsr.ErrNegativeQuantities):
		return errorResponse{"Invalid market state: negative quantities", http.StatusBadRequest}
	case errors.Is(err, lmsr.ErrInvalidLiquidity):
		return errorResponse{"Invalid liquidity parameter", http.StatusBadRequest}

	// Stellar account errors -> 400 Bad Request
	case errors.Is(err, stellar.ErrAccountNotFound):
		return errorResponse{"Stellar account not found. Please ensure the account exists and is funded.", http.StatusBadRequest}

	// Soroban RPC errors -> 502 Bad Gateway
	case errors.Is(err, soroban.ErrRPCError):
		return errorResponse{"Failed to communicate with the blockchain. Please try again later.", http.StatusBadGateway}
	case errors.Is(err, soroban.ErrSimulationFailed):
		// Check if the simulation error contains a contract error code
		errStr := err.Error()
		if strings.Contains(errStr, "Error(Contract, #") {
			return mapContractError(errStr)
		}
		return errorResponse{"Transaction simulation failed. Your parameters may be invalid.", http.StatusBadRequest}
	case errors.Is(err, soroban.ErrTransactionFailed):
		return errorResponse{"Transaction failed. Please check your parameters and try again.", http.StatusBadRequest}
	case errors.Is(err, soroban.ErrTimeout):
		return errorResponse{"Request timed out. Please try again.", http.StatusGatewayTimeout}

	// Context errors -> appropriate status
	case errors.Is(err, context.DeadlineExceeded):
		return errorResponse{"Request timed out. Please try again.", http.StatusGatewayTimeout}
	case errors.Is(err, context.Canceled):
		return errorResponse{"Request was cancelled.", http.StatusBadRequest}

	// Contract errors (from simulation) -> 400 Bad Request
	default:
		// Check for Soroban contract error codes in the error message
		errStr := err.Error()
		if strings.Contains(errStr, "Error(Contract, #") {
			return mapContractError(errStr)
		}
		return errorResponse{"An unexpected error occurred. Please try again later.", http.StatusInternalServerError}
	}
}

// mapContractError maps Soroban contract error codes to user-friendly messages.
// Error codes are defined in contracts/lmsr_market/src/error.rs
// IMPORTANT: Simulation errors may come from ANY contract in the call chain
// (market contract, SAC token, etc.), so error codes are ambiguous.
// We extract the LAST error code (innermost/root cause) and provide
// context-aware messages where codes overlap with SAC token errors.
func mapContractError(errStr string) errorResponse {
	code := extractLastErrorCode(errStr)
	switch code {
	case 15:
		return errorResponse{"Insufficient pool balance.", http.StatusBadRequest}
	case 14:
		return errorResponse{"Contract storage corrupted.", http.StatusInternalServerError}
	case 13:
		return errorResponse{"Nothing to claim. You either have no winning tokens or already claimed.", http.StatusBadRequest}
	case 12:
		return errorResponse{"Arithmetic overflow.", http.StatusBadRequest}
	case 11:
		return errorResponse{"Invalid liquidity parameter.", http.StatusBadRequest}
	case 10:
		// Code #10 is ambiguous: market contract = Unauthorized, SAC token = insufficient balance.
		// In buy/sell context, SAC errors are more likely, so use a combined message.
		return errorResponse{"Transaction failed. Check that your account has enough EURMTL and the collateral token trustline.", http.StatusBadRequest}
	case 9:
		return errorResponse{"Return amount too low.", http.StatusBadRequest}
	case 8:
		return errorResponse{"Slippage exceeded. Price moved unfavorably.", http.StatusBadRequest}
	case 7:
		return errorResponse{"Insufficient token balance.", http.StatusBadRequest}
	case 6:
		return errorResponse{"Invalid amount.", http.StatusBadRequest}
	case 5:
		return errorResponse{"Invalid outcome. Must be YES (0) or NO (1).", http.StatusBadRequest}
	case 4:
		return errorResponse{"Market has not been resolved yet.", http.StatusBadRequest}
	case 3:
		return errorResponse{"Market has already been resolved.", http.StatusConflict}
	case 2:
		return errorResponse{"Contract is not initialized.", http.StatusBadRequest}
	case 1:
		return errorResponse{"Contract is already initialized.", http.StatusConflict}
	default:
		if code < 0 {
			return errorResponse{"Contract error occurred. Please try again.", http.StatusBadRequest}
		}
		return errorResponse{fmt.Sprintf("Contract error #%d occurred.", code), http.StatusBadRequest}
	}
}

// extractLastErrorCode extracts the last Error(Contract, #N) code from a simulation error string.
// When multiple codes appear (e.g., cross-contract calls), uses the last occurrence as a heuristic.
// Returns -1 if no valid code found.
func extractLastErrorCode(errStr string) int {
	lastCode := -1
	for i := 0; i < len(errStr); i++ {
		idx := strings.Index(errStr[i:], "Error(Contract, #")
		if idx < 0 {
			break
		}
		i += idx + len("Error(Contract, #")
		end := strings.Index(errStr[i:], ")")
		if end < 0 {
			break
		}
		if code, err := strconv.Atoi(errStr[i : i+end]); err == nil {
			lastCode = code
		}
	}
	return lastCode
}

// writeError writes an error response with appropriate status code.
func (h *MarketHandler) writeError(w http.ResponseWriter, r *http.Request, err error, logContext ...any) {
	resp := mapError(err)
	logArgs := append([]any{"error", err, "status", resp.Status}, logContext...)
	h.logger.Error("request failed", logArgs...)

	var accountID string
	if r != nil {
		accountID = accountIDFromCookie(r)
	}

	w.WriteHeader(resp.Status)
	data := map[string]any{
		"ErrorCode":    resp.Status,
		"ErrorMessage": resp.Message,
		"ActiveNav":    "",
		"AccountID":    accountID,
		"Network":      h.networkName(),
	}
	if tmplErr := h.tmpl.Render(w, "error", data); tmplErr != nil {
		// Headers already sent — cannot recover, just log
		h.logger.Error("failed to render error template", "error", tmplErr)
	}
}

// handleAPIQuote returns a JSON price quote for the trade form.
func (h *MarketHandler) handleAPIQuote(w http.ResponseWriter, r *http.Request) {
	contractID := r.PathValue("id")
	if contractID == "" {
		writeJSONError(w, "contract ID required", http.StatusBadRequest)
		return
	}

	outcomeStr := r.FormValue("outcome")
	amountStr := r.FormValue("amount")

	outcome, err := model.ParseOutcome(outcomeStr)
	if err != nil {
		writeJSONError(w, "invalid outcome", http.StatusBadRequest)
		return
	}

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		writeJSONError(w, "invalid amount", http.StatusBadRequest)
		return
	}

	quote, err := h.marketService.GetQuote(r.Context(), contractID, outcome, amount)
	if err != nil {
		h.logger.Error("quote API error", "error", err, "contract_id", contractID, "outcome", outcomeStr, "amount", amountStr)
		writeJSONError(w, "quote unavailable", http.StatusBadGateway)
		return
	}

	costFloat := float64(quote.Cost) / float64(soroban.ScaleFactor)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"cost":        costFloat,
		"price_after": quote.PriceAfter,
	}); err != nil {
		h.logger.Error("failed to encode quote response", "error", err)
	}
}

// writeJSONError writes a JSON error response with proper Content-Type.
func writeJSONError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

const (
	mtlWalletAPI    = "https://eurmtl.me/remote/sep07/add"
	maxWalletURILen = 64 * 1024 // 64 KB max URI size
)

// mtlWalletClient is a dedicated HTTP client for the MTL Wallet API.
// Does not follow redirects to prevent SSRF-adjacent issues.
var mtlWalletClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// handleMTLWallet proxies a Stellar URI to the MTL Wallet API (mainnet only).
// Accepts form-encoded "uri" from frontend, forwards as JSON to eurmtl.me.
// Validates upstream response is JSON with an https:// URL before forwarding.
func (h *MarketHandler) handleMTLWallet(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(h.networkPassphrase, "Test") {
		writeJSONError(w, "MTL Wallet is only available on mainnet", http.StatusBadRequest)
		return
	}

	uri := r.FormValue("uri")
	if uri == "" || !strings.HasPrefix(uri, "web+stellar:tx?") {
		writeJSONError(w, "invalid Stellar URI", http.StatusBadRequest)
		return
	}
	if len(uri) > maxWalletURILen {
		writeJSONError(w, "URI too large", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	jsonBody, err := json.Marshal(map[string]string{"uri": uri})
	if err != nil {
		h.logger.Error("failed to marshal MTL Wallet request", "error", err)
		writeJSONError(w, "internal error", http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mtlWalletAPI, bytes.NewReader(jsonBody))
	if err != nil {
		writeJSONError(w, "internal error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := mtlWalletClient.Do(req)
	if err != nil {
		h.logger.Warn("MTL Wallet API unreachable", "error", err)
		writeJSONError(w, "MTL Wallet is temporarily unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16)) // 64 KB max response
	if err != nil {
		h.logger.Error("failed to read MTL Wallet response", "error", err)
		writeJSONError(w, "failed to read wallet response", http.StatusBadGateway)
		return
	}

	if resp.StatusCode >= 400 {
		h.logger.Warn("MTL Wallet API error", "status", resp.StatusCode, "body", string(body))
		writeJSONError(w, "MTL Wallet returned an error", http.StatusBadGateway)
		return
	}

	// Validate response is JSON with a safe URL
	var walletResp struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &walletResp); err != nil {
		h.logger.Error("MTL Wallet returned non-JSON", "body_prefix", string(body[:min(len(body), 200)]))
		writeJSONError(w, "unexpected wallet response", http.StatusBadGateway)
		return
	}
	if walletResp.URL != "" && !strings.HasPrefix(walletResp.URL, "https://") {
		h.logger.Warn("MTL Wallet returned non-https URL", "url", walletResp.URL)
		writeJSONError(w, "unexpected wallet response", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}
