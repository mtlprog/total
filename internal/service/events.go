package service

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/mtlprog/total/internal/soroban"
	"github.com/samber/hot"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// lookbackLedgers is ~24 hours at 5s/ledger.
const lookbackLedgers = 17280

const (
	eventCacheTTL  = 5 * time.Minute
	eventCacheSize = 500
)

// TradeKind represents the type of trade event.
type TradeKind string

const (
	TradeKindBuy  TradeKind = "buy"
	TradeKindSell TradeKind = "sell"
)

// TradeEvent represents a parsed buy/sell event from the contract.
type TradeEvent struct {
	Kind      TradeKind // TradeKindBuy or TradeKindSell
	User      string    // G... address
	Outcome   string    // "YES" or "NO"
	Amount    float64   // human-readable tokens
	Cost      float64   // collateral paid (buy) or received (sell)
	Timestamp time.Time // ledger close time
	Ledger    uint32
}

// EventService fetches and caches contract trade events.
type EventService struct {
	sorobanClient *soroban.Client
	logger        *slog.Logger
	cache         *hot.HotCache[string, []TradeEvent]
}

// NewEventService creates a new event service.
func NewEventService(sorobanClient *soroban.Client, logger *slog.Logger) *EventService {
	if sorobanClient == nil {
		panic("NewEventService: sorobanClient must not be nil")
	}
	if logger == nil {
		panic("NewEventService: logger must not be nil")
	}

	s := &EventService{
		sorobanClient: sorobanClient,
		logger:        logger,
	}

	s.cache = hot.NewHotCache[string, []TradeEvent](hot.LRU, eventCacheSize).
		WithTTL(eventCacheTTL).
		Build()

	return s
}

// GetTradeEvents returns trade events for a contract, using cache when available.
func (s *EventService) GetTradeEvents(ctx context.Context, contractID string) ([]TradeEvent, error) {
	cached, found, err := s.cache.Get(contractID)
	if err != nil {
		s.logger.Warn("event cache error, treating as miss", "contract_id", contractID, "error", err)
	}
	if found && err == nil {
		return slices.Clone(cached), nil
	}

	events, err := s.fetchEvents(ctx, contractID)
	if err != nil {
		return nil, err
	}

	s.cache.Set(contractID, events)
	return slices.Clone(events), nil
}

func (s *EventService) fetchEvents(ctx context.Context, contractID string) ([]TradeEvent, error) {
	latestLedger, err := s.sorobanClient.GetLatestLedger(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest ledger: %w", err)
	}

	startLedger := uint32(0)
	if latestLedger.Sequence > lookbackLedgers {
		startLedger = latestLedger.Sequence - lookbackLedgers
	}

	buyTopicXDR, err := encodeSymbolBase64("buy")
	if err != nil {
		return nil, fmt.Errorf("failed to encode buy topic: %w", err)
	}
	sellTopicXDR, err := encodeSymbolBase64("sell")
	if err != nil {
		return nil, fmt.Errorf("failed to encode sell topic: %w", err)
	}

	params := soroban.GetEventsParams{
		StartLedger: startLedger,
		Filters: []soroban.EventFilter{
			{
				Type:        "contract",
				ContractIDs: []string{contractID},
				Topics: [][]string{
					{buyTopicXDR, "*", "*"},
				},
			},
			{
				Type:        "contract",
				ContractIDs: []string{contractID},
				Topics: [][]string{
					{sellTopicXDR, "*", "*"},
				},
			},
		},
		Pagination: &soroban.EventPagination{Limit: 200},
	}

	result, err := s.sorobanClient.GetEvents(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}

	var events []TradeEvent
	var parseErrors int
	var lastParseErr error
	successfulEvents := 0
	for _, evt := range result.Events {
		if !evt.InSuccessfulContractCall {
			continue
		}
		successfulEvents++
		parsed, err := s.parseTradeEvent(evt)
		if err != nil {
			parseErrors++
			lastParseErr = err
			s.logger.Warn("failed to parse trade event", "id", evt.ID, "error", err)
			continue
		}
		events = append(events, parsed)
	}

	// If all events failed to parse, return error (likely a schema mismatch)
	if parseErrors > 0 && len(events) == 0 && successfulEvents > 0 {
		return nil, fmt.Errorf("all %d events failed to parse: %w", parseErrors, lastParseErr)
	}

	return events, nil
}

func (s *EventService) parseTradeEvent(evt soroban.ContractEvent) (TradeEvent, error) {
	if len(evt.Topic) < 3 {
		return TradeEvent{}, fmt.Errorf("expected at least 3 topics, got %d", len(evt.Topic))
	}

	// Topic[0]: symbol "buy" or "sell"
	kindVal, err := soroban.ParseReturnValue(evt.Topic[0])
	if err != nil {
		return TradeEvent{}, fmt.Errorf("failed to parse kind topic: %w", err)
	}
	if kindVal.Type != xdr.ScValTypeScvSymbol || kindVal.Sym == nil {
		return TradeEvent{}, fmt.Errorf("expected symbol topic, got %v", kindVal.Type)
	}
	kind := TradeKind(string(*kindVal.Sym))

	// Topic[1]: address (user)
	userVal, err := soroban.ParseReturnValue(evt.Topic[1])
	if err != nil {
		return TradeEvent{}, fmt.Errorf("failed to parse user topic: %w", err)
	}
	user, err := soroban.DecodeAddress(userVal)
	if err != nil {
		return TradeEvent{}, fmt.Errorf("failed to decode user address: %w", err)
	}

	// Topic[2]: u32 (outcome)
	outcomeVal, err := soroban.ParseReturnValue(evt.Topic[2])
	if err != nil {
		return TradeEvent{}, fmt.Errorf("failed to parse outcome topic: %w", err)
	}
	outcomeU32, err := soroban.DecodeU32(outcomeVal)
	if err != nil {
		return TradeEvent{}, fmt.Errorf("failed to decode outcome: %w", err)
	}
	outcome, err := soroban.U32ToOutcome(outcomeU32)
	if err != nil {
		return TradeEvent{}, fmt.Errorf("failed to convert outcome: %w", err)
	}

	// Value: tuple (amount, cost/return)
	dataVal, err := soroban.ParseReturnValue(evt.Value)
	if err != nil {
		return TradeEvent{}, fmt.Errorf("failed to parse event data: %w", err)
	}
	dataTuple, err := soroban.DecodeVec(dataVal)
	if err != nil {
		return TradeEvent{}, fmt.Errorf("failed to decode event data tuple: %w", err)
	}
	if len(dataTuple) < 2 {
		return TradeEvent{}, fmt.Errorf("expected 2 data elements, got %d", len(dataTuple))
	}

	amount, err := soroban.DecodeI128(dataTuple[0])
	if err != nil {
		return TradeEvent{}, fmt.Errorf("failed to decode amount: %w", err)
	}
	cost, err := soroban.DecodeI128(dataTuple[1])
	if err != nil {
		return TradeEvent{}, fmt.Errorf("failed to decode cost: %w", err)
	}

	ts, err := time.Parse(time.RFC3339, evt.LedgerClosedAt)
	if err != nil {
		return TradeEvent{}, fmt.Errorf("failed to parse ledger close time %q: %w", evt.LedgerClosedAt, err)
	}

	return TradeEvent{
		Kind:      kind,
		User:      user,
		Outcome:   outcome,
		Amount:    float64(amount) / float64(soroban.ScaleFactor),
		Cost:      float64(cost) / float64(soroban.ScaleFactor),
		Timestamp: ts,
		Ledger:    evt.Ledger,
	}, nil
}

// encodeSymbolBase64 encodes a symbol string as base64 XDR ScVal.
func encodeSymbolBase64(s string) (string, error) {
	val := soroban.EncodeSymbol(s)
	b, err := xdr.MarshalBase64(val)
	if err != nil {
		return "", fmt.Errorf("failed to marshal symbol: %w", err)
	}
	return b, nil
}
