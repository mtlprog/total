package soroban

import "encoding/json"

// JSON-RPC request/response types for Soroban RPC API.
// Reference: https://developers.stellar.org/docs/data/rpc/api-reference

// RPCRequest is a generic JSON-RPC 2.0 request.
type RPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// RPCResponse is a generic JSON-RPC 2.0 response.
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC error.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e.Data != "" {
		return e.Message + ": " + e.Data
	}
	return e.Message
}

// SimulateTransactionParams for simulateTransaction RPC call.
type SimulateTransactionParams struct {
	Transaction    string          `json:"transaction"`
	ResourceConfig *ResourceConfig `json:"resourceConfig,omitempty"`
}

// ResourceConfig for simulation.
type ResourceConfig struct {
	InstructionLeeway uint64 `json:"instructionLeeway,omitempty"`
}

// SimulateTransactionResult from simulateTransaction RPC call.
type SimulateTransactionResult struct {
	TransactionData string              `json:"transactionData,omitempty"`
	MinResourceFee  string              `json:"minResourceFee,omitempty"`
	Events          []string            `json:"events,omitempty"`
	Results         []SimulateResult    `json:"results,omitempty"`
	Cost            *SimulateCost       `json:"cost,omitempty"`
	RestorePreamble *RestorePreamble    `json:"restorePreamble,omitempty"`
	StateChanges    []LedgerEntryChange `json:"stateChanges,omitempty"`
	LatestLedger    uint32              `json:"latestLedger"`
	Error           string              `json:"error,omitempty"`
}

// SimulateResult contains the result of a simulated operation.
type SimulateResult struct {
	Auth []string `json:"auth,omitempty"`
	XDR  string   `json:"xdr,omitempty"`
}

// SimulateCost contains resource costs from simulation.
type SimulateCost struct {
	CPUInsns string `json:"cpuInsns"`
	MemBytes string `json:"memBytes"`
}

// RestorePreamble contains info about entries that need restoration.
type RestorePreamble struct {
	TransactionData string `json:"transactionData"`
	MinResourceFee  string `json:"minResourceFee"`
}

// LedgerEntryChange represents a state change.
type LedgerEntryChange struct {
	Type   string `json:"type"`
	Key    string `json:"key"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// SendTransactionParams for sendTransaction RPC call.
type SendTransactionParams struct {
	Transaction string `json:"transaction"`
}

// SendTransactionResult from sendTransaction RPC call.
type SendTransactionResult struct {
	Status       string `json:"status"`
	Hash         string `json:"hash"`
	LatestLedger uint32 `json:"latestLedger"`
	ErrorResult  string `json:"errorResultXdr,omitempty"`
}

// TransactionStatus values.
const (
	TxStatusPending   = "PENDING"
	TxStatusDuplicate = "DUPLICATE"
	TxStatusTryAgain  = "TRY_AGAIN_LATER"
	TxStatusError     = "ERROR"
)

// GetTransactionParams for getTransaction RPC call.
type GetTransactionParams struct {
	Hash string `json:"hash"`
}

// GetTransactionResult from getTransaction RPC call.
type GetTransactionResult struct {
	Status                string `json:"status"`
	LatestLedger          uint32 `json:"latestLedger"`
	LatestLedgerCloseTime string `json:"latestLedgerCloseTime"`
	OldestLedger          uint32 `json:"oldestLedger"`
	OldestLedgerCloseTime string `json:"oldestLedgerCloseTime"`
	// Fields present when status is SUCCESS or FAILED
	Ledger           uint32 `json:"ledger,omitempty"`
	CreatedAt        string `json:"createdAt,omitempty"`
	ApplicationOrder int    `json:"applicationOrder,omitempty"`
	FeeBump          bool   `json:"feeBump,omitempty"`
	EnvelopeXdr      string `json:"envelopeXdr,omitempty"`
	ResultXdr        string `json:"resultXdr,omitempty"`
	ResultMetaXdr    string `json:"resultMetaXdr,omitempty"`
	ReturnValue      string `json:"returnValue,omitempty"`
}

// TransactionStatusResult values.
const (
	TxResultNotFound = "NOT_FOUND"
	TxResultSuccess  = "SUCCESS"
	TxResultFailed   = "FAILED"
)

// GetLedgerEntriesParams for getLedgerEntries RPC call.
type GetLedgerEntriesParams struct {
	Keys []string `json:"keys"`
}

// GetLedgerEntriesResult from getLedgerEntries RPC call.
type GetLedgerEntriesResult struct {
	Entries      []LedgerEntry `json:"entries,omitempty"`
	LatestLedger uint32        `json:"latestLedger"`
}

// LedgerEntry represents a ledger entry.
type LedgerEntry struct {
	Key                   string `json:"key"`
	XDR                   string `json:"xdr"`
	LastModifiedLedgerSeq uint32 `json:"lastModifiedLedgerSeq"`
	LiveUntilLedgerSeq    uint32 `json:"liveUntilLedgerSeq,omitempty"`
}

// GetHealthResult from getHealth RPC call.
type GetHealthResult struct {
	Status                string `json:"status"`
	LatestLedger          uint32 `json:"latestLedger"`
	OldestLedger          uint32 `json:"oldestLedger"`
	LedgerRetentionWindow uint32 `json:"ledgerRetentionWindow"`
}

// GetNetworkResult from getNetwork RPC call.
type GetNetworkResult struct {
	FriendbotURL    string `json:"friendbotUrl,omitempty"`
	Passphrase      string `json:"passphrase"`
	ProtocolVersion int    `json:"protocolVersion"`
}

// GetLatestLedgerResult from getLatestLedger RPC call.
type GetLatestLedgerResult struct {
	ID              string `json:"id"`
	ProtocolVersion int    `json:"protocolVersion"`
	Sequence        uint32 `json:"sequence"`
}

// Outcome constants matching Soroban contract.
const (
	OutcomeYes uint32 = 0
	OutcomeNo  uint32 = 1
)

// ScaleFactor for fixed-point arithmetic (10^7 like Stellar).
const ScaleFactor int64 = 10_000_000
