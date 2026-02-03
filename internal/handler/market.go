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

	"github.com/mtlprog/total/internal/lmsr"
	"github.com/mtlprog/total/internal/model"
	"github.com/mtlprog/total/internal/service"
	"github.com/mtlprog/total/internal/soroban"
	"github.com/mtlprog/total/internal/template"
	"github.com/stellar/go-stellar-sdk/keypair"
)

// MarketHandler handles HTTP requests for prediction markets.
type MarketHandler struct {
	marketService   *service.MarketService
	tmpl            *template.Template
	oraclePublicKey string
	contractIDs     []string // In-memory list of known contract IDs
	contractIDsMu   sync.RWMutex
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
		contractIDs:     []string{},
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
	mux.HandleFunc("POST /market/{id}/sell", h.handleBuildSellTx)
	mux.HandleFunc("POST /market/{id}/resolve", h.handleResolveMarket)
	mux.HandleFunc("POST /market/{id}/claim", h.handleBuildClaimTx)
	mux.HandleFunc("GET /health", h.handleHealth)
}

// handleListMarkets renders the list of all known contracts.
func (h *MarketHandler) handleListMarkets(w http.ResponseWriter, r *http.Request) {
	h.contractIDsMu.RLock()
	ids := make([]string, len(h.contractIDs))
	copy(ids, h.contractIDs)
	h.contractIDsMu.RUnlock()

	// Simple list of contract IDs for now
	// TODO: Query contract state for each market
	data := map[string]any{
		"ContractIDs":     ids,
		"OraclePublicKey": h.oraclePublicKey,
	}

	if err := h.tmpl.Render(w, "markets", data); err != nil {
		h.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleMarketDetail renders a single market's detail page.
func (h *MarketHandler) handleMarketDetail(w http.ResponseWriter, r *http.Request) {
	contractID := r.PathValue("id")
	if contractID == "" {
		http.Error(w, "Contract ID required", http.StatusBadRequest)
		return
	}

	// TODO: Query contract state to get market details
	data := map[string]any{
		"ContractID":      contractID,
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
		"Result":     result,
		"ContractID": contractID,
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
		"Result":     result,
		"ContractID": contractID,
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
		"Result":     result,
		"ContractID": contractID,
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
		"Result":     result,
		"ContractID": contractID,
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

// AddContractID adds a contract ID to the tracked list (thread-safe).
func (h *MarketHandler) AddContractID(id string) {
	h.contractIDsMu.Lock()
	defer h.contractIDsMu.Unlock()
	h.contractIDs = append(h.contractIDs, id)
}

// SetMarketIDs sets the list of tracked contract IDs (thread-safe).
// Kept for backward compatibility with main.go
func (h *MarketHandler) SetMarketIDs(ids []string) {
	h.contractIDsMu.Lock()
	defer h.contractIDsMu.Unlock()
	h.contractIDs = make([]string, len(ids))
	copy(h.contractIDs, ids)
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

	// Unknown errors -> 500 Internal Server Error
	default:
		return errorResponse{"An unexpected error occurred. Please try again later.", http.StatusInternalServerError}
	}
}

// writeError writes an error response with appropriate status code.
func (h *MarketHandler) writeError(w http.ResponseWriter, err error, logContext ...any) {
	resp := mapError(err)
	logArgs := append([]any{"error", err, "status", resp.Status}, logContext...)
	h.logger.Error("request failed", logArgs...)
	http.Error(w, resp.Message, resp.Status)
}
