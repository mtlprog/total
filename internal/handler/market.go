package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

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
	ipfsClient *ipfs.Client,
	tmpl *template.Template,
	oraclePublicKey string,
	networkPassphrase string,
	logger *slog.Logger,
) *MarketHandler {
	return &MarketHandler{
		marketService:     marketService,
		factoryService:    factoryService,
		ipfsClient:        ipfsClient,
		tmpl:              tmpl,
		oraclePublicKey:   oraclePublicKey,
		networkPassphrase: networkPassphrase,
		logger:            logger,
	}
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
	mux.HandleFunc("GET /oracle", h.handleOracleAdmin)
	mux.HandleFunc("GET /deploy", h.handleRedirectToOracle)
	mux.HandleFunc("POST /deploy", h.handleBuildDeployTx)
	mux.HandleFunc("GET /health", h.handleHealth)
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
	YesAsset       string
	NoAsset        string
	MetadataError  string // Non-empty when IPFS metadata failed to load
}

// shortID formats an ID as "first8...last8" for display.
func shortID(id string) string {
	if len(id) <= 19 {
		return id
	}
	return id[:8] + "..." + id[len(id)-8:]
}

// handleListMarkets renders the list of all markets from factory.
func (h *MarketHandler) handleListMarkets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.factoryService == nil || !h.factoryService.HasFactory() {
		data := map[string]any{
			"Markets":         []MarketView{},
			"OraclePublicKey": h.oraclePublicKey,
			"Error":           "Factory contract not configured",
			"ActiveNav":       "markets",
			"Network":         h.networkName(),
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
	}

	if err := h.tmpl.Render(w, "markets", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// buildMarketViews converts market states to views, fetching metadata in parallel.
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
				YesAsset:     "YES",
				NoAsset:      "NO",
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
		h.writeError(w, err, "contract_id", contractID)
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
		YesAsset: "YES",
		NoAsset:  "NO",
	}

	if state.Resolved {
		market.Resolution = model.OutcomeYes // TODO: get actual resolution from contract
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
		}
		market.MetadataHash = state.MetadataHash
	} else {
		market.Question = "Market " + shortID(contractID)
	}

	data := map[string]any{
		"Market":          &market,
		"OraclePublicKey": h.oraclePublicKey,
		"BarChart":        "", // TODO: add bar chart
		"PriceChart":      "", // TODO: add price history chart
		"ActiveNav":       "markets",
		"Network":         h.networkName(),
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
		h.writeError(w, err, "contract_id", contractID, "outcome", outcome, "amount", amount)
		return
	}

	// Return quote page
	data := map[string]any{
		"Quote":      quote,
		"ContractID": contractID,
		"ActiveNav":  "markets",
		"Network":    h.networkName(),
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
		h.writeError(w, err, "contract_id", contractID, "outcome", outcome, "amount", amount)
		return
	}

	// Render XDR result page
	data := map[string]any{
		"Result":    result,
		"MarketID":  contractID,
		"ActiveNav": "markets",
		"Network":   h.networkName(),
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
		h.writeError(w, err, "contract_id", contractID, "outcome", outcome, "amount", amount)
		return
	}

	// Render XDR result page
	data := map[string]any{
		"Result":    result,
		"MarketID":  contractID,
		"ActiveNav": "markets",
		"Network":   h.networkName(),
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
		h.writeError(w, err, "contract_id", contractID, "outcome", outcome)
		return
	}

	data := map[string]any{
		"Result":    result,
		"MarketID":  contractID,
		"ActiveNav": "oracle",
		"Network":   h.networkName(),
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
		h.writeError(w, err, "contract_id", contractID, "user_public_key", userPubKey)
		return
	}

	data := map[string]any{
		"Result":    result,
		"MarketID":  contractID,
		"ActiveNav": "markets",
		"Network":   h.networkName(),
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
		h.writeError(w, err, "contract_id", contractID, "oracle_public_key", oraclePubKey)
		return
	}

	data := map[string]any{
		"Result":    result,
		"MarketID":  contractID,
		"ActiveNav": "oracle",
		"Network":   h.networkName(),
	}

	if err := h.tmpl.Render(w, "transaction", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
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

	if h.factoryService != nil && h.factoryService.HasFactory() {
		factoryContract = h.factoryService.FactoryContractID()

		// Get all markets for the dropdowns
		contractIDs, err := h.factoryService.ListMarkets(ctx)
		if err != nil {
			h.logger.Warn("failed to list markets for oracle admin", "error", err)
		} else {
			states, err := h.factoryService.GetMarketStates(ctx, contractIDs)
			if err != nil {
				h.logger.Warn("failed to get market states for oracle admin", "error", err)
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
		"ActiveNav":             "oracle",
		"Network":               h.networkName(),
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
		h.writeError(w, err, "liquidity_param", liquidityParam, "metadata_hash", metadataHash)
		return
	}

	data := map[string]any{
		"Result":    result,
		"MarketID":  "new",
		"ActiveNav": "oracle",
		"Network":   h.networkName(),
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
func mapContractError(errStr string) errorResponse {
	// Extract error code from string like "Error(Contract, #13)"
	switch {
	case strings.Contains(errStr, "#1"):
		return errorResponse{"Contract is already initialized.", http.StatusConflict}
	case strings.Contains(errStr, "#2"):
		return errorResponse{"Contract is not initialized.", http.StatusBadRequest}
	case strings.Contains(errStr, "#3"):
		return errorResponse{"Market has already been resolved.", http.StatusConflict}
	case strings.Contains(errStr, "#4"):
		return errorResponse{"Market has not been resolved yet.", http.StatusBadRequest}
	case strings.Contains(errStr, "#5"):
		return errorResponse{"Invalid outcome. Must be YES (0) or NO (1).", http.StatusBadRequest}
	case strings.Contains(errStr, "#6"):
		return errorResponse{"Invalid amount.", http.StatusBadRequest}
	case strings.Contains(errStr, "#7"):
		return errorResponse{"Insufficient token balance.", http.StatusBadRequest}
	case strings.Contains(errStr, "#8"):
		return errorResponse{"Slippage exceeded. Price moved unfavorably.", http.StatusBadRequest}
	case strings.Contains(errStr, "#9"):
		return errorResponse{"Return amount too low.", http.StatusBadRequest}
	case strings.Contains(errStr, "#10"):
		return errorResponse{"Unauthorized. Only the oracle can perform this action.", http.StatusForbidden}
	case strings.Contains(errStr, "#11"):
		return errorResponse{"Invalid liquidity parameter.", http.StatusBadRequest}
	case strings.Contains(errStr, "#12"):
		return errorResponse{"Arithmetic overflow.", http.StatusBadRequest}
	case strings.Contains(errStr, "#13"):
		return errorResponse{"Nothing to claim. You either have no winning tokens or already claimed.", http.StatusBadRequest}
	case strings.Contains(errStr, "#14"):
		return errorResponse{"Contract storage corrupted.", http.StatusInternalServerError}
	case strings.Contains(errStr, "#15"):
		return errorResponse{"Insufficient pool balance.", http.StatusBadRequest}
	default:
		return errorResponse{fmt.Sprintf("Contract error occurred: %s", errStr), http.StatusBadRequest}
	}
}

// writeError writes an error response with appropriate status code.
func (h *MarketHandler) writeError(w http.ResponseWriter, err error, logContext ...any) {
	resp := mapError(err)
	logArgs := append([]any{"error", err, "status", resp.Status}, logContext...)
	h.logger.Error("request failed", logArgs...)
	http.Error(w, resp.Message, resp.Status)
}
