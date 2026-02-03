package handler

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mtlprog/total/internal/chart"
	"github.com/mtlprog/total/internal/model"
	"github.com/mtlprog/total/internal/service"
	"github.com/mtlprog/total/internal/template"
)

// MarketHandler handles HTTP requests for prediction markets.
type MarketHandler struct {
	marketService   *service.MarketService
	tmpl            *template.Template
	oraclePublicKey string
	marketIDs       []string // In-memory list of known market IDs
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
	markets, err := h.marketService.ListMarkets(r.Context(), h.marketIDs)
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

	// Get price history
	history, _ := h.marketService.GetPriceHistory(r.Context(), marketID, 100)

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
	outcome := r.FormValue("outcome")
	amountStr := r.FormValue("amount")

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		http.Error(w, "Invalid amount", http.StatusBadRequest)
		return
	}

	quote, err := h.marketService.GetQuote(r.Context(), marketID, outcome, amount)
	if err != nil {
		h.logger.Error("failed to get quote", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	outcome := r.FormValue("outcome")
	amountStr := r.FormValue("amount")

	// Validate public key format
	if len(userPubKey) != 56 || !strings.HasPrefix(userPubKey, "G") {
		http.Error(w, "Invalid Stellar public key", http.StatusBadRequest)
		return
	}

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		http.Error(w, "Invalid amount", http.StatusBadRequest)
		return
	}

	req := model.BuyRequest{
		UserPublicKey: userPubKey,
		MarketID:      marketID,
		Outcome:       outcome,
		ShareAmount:   amount,
	}

	result, err := h.marketService.BuildBuyTx(r.Context(), req)
	if err != nil {
		h.logger.Error("failed to build buy tx", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		"OraclePublicKey":      h.oraclePublicKey,
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

	question := html.EscapeString(strings.TrimSpace(r.FormValue("question")))
	description := html.EscapeString(strings.TrimSpace(r.FormValue("description")))
	closeTimeStr := r.FormValue("close_time")
	liquidityStr := r.FormValue("liquidity_param")

	if question == "" {
		http.Error(w, "Question is required", http.StatusBadRequest)
		return
	}

	closeTime, err := time.Parse("2006-01-02T15:04", closeTimeStr)
	if err != nil {
		http.Error(w, "Invalid close time", http.StatusBadRequest)
		return
	}

	liquidity, err := strconv.ParseFloat(liquidityStr, 64)
	if err != nil || liquidity <= 0 {
		liquidity = 100.0
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Add market ID to our list
	h.marketIDs = append(h.marketIDs, marketID)

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
	outcome := r.FormValue("outcome")

	if outcome != "YES" && outcome != "NO" {
		http.Error(w, "Invalid outcome", http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
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

// AddMarketID adds a market ID to the tracked list.
func (h *MarketHandler) AddMarketID(id string) {
	h.marketIDs = append(h.marketIDs, id)
}

// SetMarketIDs sets the list of tracked market IDs.
func (h *MarketHandler) SetMarketIDs(ids []string) {
	h.marketIDs = ids
}
