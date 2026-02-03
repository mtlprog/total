package handler

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/mtlprog/total/internal/repository"
	"github.com/mtlprog/total/internal/template"
)

type Handler struct {
	repo *repository.Repository
	tmpl *template.Template
}

func New(repo *repository.Repository, tmpl *template.Template) (*Handler, error) {
	if repo == nil {
		return nil, fmt.Errorf("repository is nil")
	}
	if tmpl == nil {
		return nil, fmt.Errorf("template is nil")
	}
	return &Handler{
		repo: repo,
		tmpl: tmpl,
	}, nil
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /", h.handleIndex)
	mux.HandleFunc("GET /health", h.handleHealth)
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	examples, err := h.repo.ListExamples(r.Context())
	if err != nil {
		slog.Error("failed to list examples", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Examples": examples,
	}

	if err := h.tmpl.Render(w, "index", data); err != nil {
		slog.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
