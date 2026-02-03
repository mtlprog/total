package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mtlprog/total/internal/chart"
	"github.com/mtlprog/total/internal/model"
	"github.com/mtlprog/total/internal/service"
	"github.com/mtlprog/total/internal/template"
	"github.com/stellar/go-stellar-sdk/keypair"
)

// MarketHandler handles HTTP requests for prediction markets.
type MarketHandler struct {
	marketService   *service.MarketService
	tmpl            *template.Template
	oraclePublicKey string
	marketIDs       []string // In-memory list of known market IDs
	marketIDsMu     sync.RWMutex
	logger          *slog.Logger
}

// NewMarketHandler creates a new market handler.
func NewMarketHandler(
	marketService *service.MarketService,
	tmpl *template.Template,
	oraclePublicKey string,
	logger *slog.Logger,
) *MarketHandler {
	return &MarketHandler{
		marketService:   marketService,
		tmpl:            tmpl,
		oraclePublicKey: oraclePublicKey,
		marketIDs:       []string{},
		logger:          logger,
	}
}

// RegisterRoutes registers market routes.
func (h *MarketHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /", h.handleListMarkets)
	mux.HandleFunc("GET /markets", h.handleListMarkets)
	mux.HandleFunc("GET /market/{id}", h.handleMarketDetail)
	mux.HandleFunc("POST /market/{id}/quote", h.handleGetQuote)
	mux.HandleFunc("POST /market/{id}/buy", h.handleBuildBuyTx)
	mux.HandleFunc("GET /create", h.handleCreateForm)
	mux.HandleFunc("POST /create", h.handleCreateMarket)
	mux.HandleFunc("POST /market/{id}/resolve", h.handleResolveMarket)
	mux.HandleFunc("GET /health", h.handleHealth)
}

// handleListMarkets renders the list of all markets.
func (h *MarketHandler) handleListMarkets(w http.ResponseWriter, r *http.Request) {
	h.marketIDsMu.RLock()
	ids := make([]string, len(h.marketIDs))
	copy(ids, h.marketIDs)
	h.marketIDsMu.RUnlock()

	markets, err := h.marketService.ListMarkets(r.Context(), ids)
	if err != nil {
		h.logger.Error("failed to list markets", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Markets":         markets,
		"OraclePublicKey": h.oraclePublicKey,
	}

	if err := h.tmpl.Render(w, "markets", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleMarketDetail renders a single market's detail page.
func (h *MarketHandler) handleMarketDetail(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	if marketID == "" {
		http.Error(w, "Market ID required", http.StatusBadRequest)
		return
	}

	market, err := h.marketService.GetMarket(r.Context(), marketID)
	if err != nil {
		if err == service.ErrMarketNotFound {
			http.Error(w, "Market not found", http.StatusNotFound)
			return
		}
		h.logger.Error("failed to get market", "id", marketID, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Get price history (log error but don't fail)
	history, err := h.marketService.GetPriceHistory(r.Context(), marketID, 100)
	if err != nil {
		h.logger.Warn("failed to get price history", "id", marketID, "error", err)
		history = nil
	}

	// Generate chart
	priceChart := chart.RenderPriceChart(history, chart.DefaultWidth, chart.DefaultHeight)
	barChart := chart.RenderSimpleBar(market.PriceYes, market.PriceNo, 50)

	data := map[string]any{
		"Market":          market,
		"PriceChart":      priceChart,
		"BarChart":        barChart,
		"OraclePublicKey": h.oraclePublicKey,
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

	marketID := r.PathValue("id")
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

	quote, err := h.marketService.GetQuote(r.Context(), marketID, outcome, amount)
	if err != nil {
		h.logger.Error("failed to get quote", "error", err)
		http.Error(w, userFriendlyError(err), http.StatusBadRequest)
		return
	}

	// Return quote page
	data := map[string]any{
		"Quote":    quote,
		"MarketID": marketID,
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

	marketID := r.PathValue("id")
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

	// Parse slippage (default 1%)
	slippage := 0.01
	if slippageStr != "" {
		if s, err := strconv.ParseFloat(slippageStr, 64); err == nil && s > 0 && s < 1 {
			slippage = s
		}
	}

	req := model.BuyRequest{
		UserPublicKey: userPubKey,
		MarketID:      marketID,
		Outcome:       outcome,
		ShareAmount:   amount,
		Slippage:      slippage,
	}

	result, err := h.marketService.BuildBuyTx(r.Context(), req)
	if err != nil {
		h.logger.Error("failed to build buy tx", "error", err)
		http.Error(w, userFriendlyError(err), http.StatusBadRequest)
		return
	}

	// Render XDR result page
	data := map[string]any{
		"Result":   result,
		"MarketID": marketID,
	}

	if err := h.tmpl.Render(w, "transaction", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleCreateForm renders the market creation form.
func (h *MarketHandler) handleCreateForm(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"OraclePublicKey":       h.oraclePublicKey,
		"DefaultLiquidityParam": 100.0,
	}

	if err := h.tmpl.Render(w, "create", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleCreateMarket creates a new market.
func (h *MarketHandler) handleCreateMarket(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	// Don't HTML escape - templates handle escaping automatically
	question := strings.TrimSpace(r.FormValue("question"))
	description := strings.TrimSpace(r.FormValue("description"))
	closeTimeStr := r.FormValue("close_time")
	liquidityStr := r.FormValue("liquidity_param")

	// Validate question
	if question == "" {
		http.Error(w, "Question is required", http.StatusBadRequest)
		return
	}
	if len(question) > model.MaxQuestionLength {
		http.Error(w, fmt.Sprintf("Question exceeds maximum length (%d characters)", model.MaxQuestionLength), http.StatusBadRequest)
		return
	}
	if len(description) > model.MaxDescriptionLength {
		http.Error(w, fmt.Sprintf("Description exceeds maximum length (%d characters)", model.MaxDescriptionLength), http.StatusBadRequest)
		return
	}

	closeTime, err := time.Parse("2006-01-02T15:04", closeTimeStr)
	if err != nil {
		http.Error(w, "Invalid close time format", http.StatusBadRequest)
		return
	}

	liquidity, err := strconv.ParseFloat(liquidityStr, 64)
	if err != nil || liquidity <= 0 {
		http.Error(w, "Invalid liquidity parameter: must be a positive number", http.StatusBadRequest)
		return
	}

	req := model.CreateMarketRequest{
		Question:        question,
		Description:     description,
		CloseTime:       closeTime,
		LiquidityParam:  liquidity,
		OraclePublicKey: h.oraclePublicKey,
	}

	result, marketID, err := h.marketService.CreateMarket(r.Context(), req)
	if err != nil {
		h.logger.Error("failed to create market", "error", err)
		http.Error(w, userFriendlyError(err), http.StatusBadRequest)
		return
	}

	// Add market ID to our list (thread-safe)
	h.marketIDsMu.Lock()
	h.marketIDs = append(h.marketIDs, marketID)
	h.marketIDsMu.Unlock()

	data := map[string]any{
		"Result":   result,
		"MarketID": marketID,
		"IsCreate": true,
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

	marketID := r.PathValue("id")
	outcomeStr := r.FormValue("outcome")

	outcome, err := model.ParseOutcome(outcomeStr)
	if err != nil {
		http.Error(w, "Invalid outcome: must be YES or NO", http.StatusBadRequest)
		return
	}

	req := model.ResolveRequest{
		MarketID:        marketID,
		WinningOutcome:  outcome,
		OraclePublicKey: h.oraclePublicKey,
	}

	result, err := h.marketService.BuildResolveTx(r.Context(), req)
	if err != nil {
		h.logger.Error("failed to resolve market", "error", err)
		http.Error(w, userFriendlyError(err), http.StatusBadRequest)
		return
	}

	data := map[string]any{
		"Result":   result,
		"MarketID": marketID,
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

// AddMarketID adds a market ID to the tracked list (thread-safe).
func (h *MarketHandler) AddMarketID(id string) {
	h.marketIDsMu.Lock()
	defer h.marketIDsMu.Unlock()
	h.marketIDs = append(h.marketIDs, id)
}

// SetMarketIDs sets the list of tracked market IDs (thread-safe).
func (h *MarketHandler) SetMarketIDs(ids []string) {
	h.marketIDsMu.Lock()
	defer h.marketIDsMu.Unlock()
	h.marketIDs = make([]string, len(ids))
	copy(h.marketIDs, ids)
}

// userFriendlyError converts internal errors to user-friendly messages.
func userFriendlyError(err error) string {
	switch err {
	case service.ErrMarketNotFound:
		return "Market not found"
	case service.ErrMarketResolved:
		return "Market has already been resolved"
	case service.ErrInvalidOutcome:
		return "Invalid outcome: must be YES or NO"
	case service.ErrIPFSNotConfigured:
		return "IPFS is not configured"
	case model.ErrEmptyQuestion:
		return "Question is required"
	case model.ErrQuestionTooLong:
		return fmt.Sprintf("Question exceeds maximum length (%d characters)", model.MaxQuestionLength)
	case model.ErrDescriptionTooLong:
		return fmt.Sprintf("Description exceeds maximum length (%d characters)", model.MaxDescriptionLength)
	case model.ErrInvalidLiquidityParam:
		return "Liquidity parameter must be a positive number"
	case model.ErrInvalidShareAmount:
		return "Share amount must be a positive number"
	case model.ErrCloseTimeInPast:
		return "Close time must be in the future"
	case model.ErrInvalidPublicKey:
		return "Invalid Stellar public key format"
	default:
		return "An error occurred. Please try again."
	}
}
