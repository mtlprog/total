package model

import "time"

// NewMarketMetadata creates a new MarketMetadata with required fields validated.
// Question is required and must not exceed MaxQuestionLength.
func NewMarketMetadata(question string) (*MarketMetadata, error) {
	m := &MarketMetadata{
		Question:  question,
		CreatedAt: time.Now().UTC(),
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

// MarketMetadata is the JSON structure stored in IPFS.
// This contains human-readable market information.
type MarketMetadata struct {
	Question         string    `json:"question"`
	Description      string    `json:"description"`
	ResolutionSource string    `json:"resolution_source,omitempty"`
	Category         string    `json:"category,omitempty"`
	EndDate          time.Time `json:"end_date,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	CreatedBy        string    `json:"created_by,omitempty"`
}

// Validate checks that required metadata fields are present.
func (m *MarketMetadata) Validate() error {
	if m.Question == "" {
		return ErrEmptyQuestion
	}
	if len(m.Question) > MaxQuestionLength {
		return ErrQuestionTooLong
	}
	if len(m.Description) > MaxDescriptionLength {
		return ErrDescriptionTooLong
	}
	return nil
}
