package model

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewMarketMetadata(t *testing.T) {
	tests := []struct {
		name     string
		question string
		wantErr  error
	}{
		{
			name:     "valid question",
			question: "Will BTC reach $100k by end of 2025?",
			wantErr:  nil,
		},
		{
			name:     "empty question",
			question: "",
			wantErr:  ErrEmptyQuestion,
		},
		{
			name:     "question at max length",
			question: strings.Repeat("a", MaxQuestionLength),
			wantErr:  nil,
		},
		{
			name:     "question exceeds max length",
			question: strings.Repeat("a", MaxQuestionLength+1),
			wantErr:  ErrQuestionTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := NewMarketMetadata(tt.question)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("NewMarketMetadata() error = %v, wantErr %v", err, tt.wantErr)
				}
				if meta != nil {
					t.Error("NewMarketMetadata() should return nil when error")
				}
			} else {
				if err != nil {
					t.Errorf("NewMarketMetadata() unexpected error = %v", err)
				}
				if meta == nil {
					t.Error("NewMarketMetadata() should return non-nil metadata")
				} else {
					if meta.Question != tt.question {
						t.Errorf("NewMarketMetadata() Question = %v, want %v", meta.Question, tt.question)
					}
					if meta.CreatedAt.IsZero() {
						t.Error("NewMarketMetadata() should set CreatedAt")
					}
				}
			}
		})
	}
}

func TestMarketMetadata_Validate(t *testing.T) {
	tests := []struct {
		name    string
		meta    MarketMetadata
		wantErr error
	}{
		{
			name: "valid metadata",
			meta: MarketMetadata{
				Question:    "Will X happen?",
				Description: "Some description",
				CreatedAt:   time.Now(),
			},
			wantErr: nil,
		},
		{
			name: "empty question",
			meta: MarketMetadata{
				Question:  "",
				CreatedAt: time.Now(),
			},
			wantErr: ErrEmptyQuestion,
		},
		{
			name: "question too long",
			meta: MarketMetadata{
				Question:  strings.Repeat("a", MaxQuestionLength+1),
				CreatedAt: time.Now(),
			},
			wantErr: ErrQuestionTooLong,
		},
		{
			name: "description too long",
			meta: MarketMetadata{
				Question:    "Valid question?",
				Description: strings.Repeat("a", MaxDescriptionLength+1),
				CreatedAt:   time.Now(),
			},
			wantErr: ErrDescriptionTooLong,
		},
		{
			name: "question at boundary",
			meta: MarketMetadata{
				Question:  strings.Repeat("a", MaxQuestionLength),
				CreatedAt: time.Now(),
			},
			wantErr: nil,
		},
		{
			name: "description at boundary",
			meta: MarketMetadata{
				Question:    "Valid question?",
				Description: strings.Repeat("a", MaxDescriptionLength),
				CreatedAt:   time.Now(),
			},
			wantErr: nil,
		},
		{
			name: "whitespace only question",
			meta: MarketMetadata{
				Question:  "   ",
				CreatedAt: time.Now(),
			},
			wantErr: nil, // Current implementation doesn't trim whitespace
		},
		{
			name: "valid with all optional fields",
			meta: MarketMetadata{
				Question:         "Will X happen?",
				Description:      "Detailed description",
				ResolutionSource: "coinbase.com",
				Category:         "crypto",
				EndDate:          time.Now().Add(24 * time.Hour),
				CreatedAt:        time.Now(),
				CreatedBy:        "GABC...",
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.meta.Validate()
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}
