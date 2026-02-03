package soroban

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/txnbuild"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// ContractInvoker builds and simulates Soroban contract invocations.
type ContractInvoker struct {
	client            *Client
	networkPassphrase string
	baseFee           int64
}

// NewContractInvoker creates a new contract invoker.
func NewContractInvoker(client *Client, networkPassphrase string, baseFee int64) *ContractInvoker {
	return &ContractInvoker{
		client:            client,
		networkPassphrase: networkPassphrase,
		baseFee:           baseFee,
	}
}

// InvokeParams contains parameters for invoking a contract function.
type InvokeParams struct {
	SourceAccount txnbuild.Account
	ContractID    string
	FunctionName  string
	Args          []xdr.ScVal
	Auth          []xdr.SorobanAuthorizationEntry
}

// BuildInvokeTx builds an InvokeHostFunction transaction.
// Returns the unsigned transaction XDR ready for simulation.
func (ci *ContractInvoker) BuildInvokeTx(ctx context.Context, params InvokeParams) (string, error) {
	// Parse contract ID to contract address
	contractIDBytes, err := strkey.Decode(strkey.VersionByteContract, params.ContractID)
	if err != nil {
		return "", fmt.Errorf("invalid contract ID: %w", err)
	}

	var contractID xdr.ContractId
	copy(contractID[:], contractIDBytes)

	contractAddress := xdr.ScAddress{
		Type:       xdr.ScAddressTypeScAddressTypeContract,
		ContractId: &contractID,
	}

	// Build the host function
	invokeArgs := xdr.InvokeContractArgs{
		ContractAddress: contractAddress,
		FunctionName:    xdr.ScSymbol(params.FunctionName),
		Args:            params.Args,
	}

	hostFunc := xdr.HostFunction{
		Type:           xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
		InvokeContract: &invokeArgs,
	}

	op := &txnbuild.InvokeHostFunction{
		HostFunction: hostFunc,
		Auth:         params.Auth,
	}

	tx, err := txnbuild.NewTransaction(
		txnbuild.TransactionParams{
			SourceAccount:        params.SourceAccount,
			IncrementSequenceNum: true,
			Operations:           []txnbuild.Operation{op},
			BaseFee:              ci.baseFee,
			Preconditions: txnbuild.Preconditions{
				TimeBounds: txnbuild.NewTimeout(300),
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to build transaction: %w", err)
	}

	xdrBytes, err := tx.Base64()
	if err != nil {
		return "", fmt.Errorf("failed to encode transaction: %w", err)
	}

	return xdrBytes, nil
}

// SimulateAndPrepare simulates a transaction and returns it with resources attached.
func (ci *ContractInvoker) SimulateAndPrepare(ctx context.Context, txXDR string) (string, error) {
	simResult, err := ci.client.SimulateTransaction(ctx, txXDR)
	if err != nil {
		return "", fmt.Errorf("simulation failed: %w", err)
	}

	if simResult.Error != "" {
		return "", fmt.Errorf("simulation error: %s", simResult.Error)
	}

	// Parse the original transaction
	var txEnvelope xdr.TransactionEnvelope
	err = xdr.SafeUnmarshalBase64(txXDR, &txEnvelope)
	if err != nil {
		return "", fmt.Errorf("failed to parse transaction: %w", err)
	}

	// Parse the soroban transaction data from simulation
	var sorobanData xdr.SorobanTransactionData
	if simResult.TransactionData != "" {
		err = xdr.SafeUnmarshalBase64(simResult.TransactionData, &sorobanData)
		if err != nil {
			return "", fmt.Errorf("failed to parse soroban data: %w", err)
		}
	}

	// Get the transaction from envelope
	if txEnvelope.Type != xdr.EnvelopeTypeEnvelopeTypeTx {
		return "", fmt.Errorf("unsupported envelope type: %v", txEnvelope.Type)
	}

	tx := &txEnvelope.V1.Tx

	// Set the soroban data as an extension
	tx.Ext = xdr.TransactionExt{
		V:           1,
		SorobanData: &sorobanData,
	}

	// Update the fee to include resource fee
	resourceFee, err := strconv.ParseInt(simResult.MinResourceFee, 10, 64)
	if err != nil {
		return "", fmt.Errorf("failed to parse resource fee: %w", err)
	}
	tx.Fee = xdr.Uint32(int64(tx.Fee) + resourceFee)

	// Update auth if provided by simulation
	if len(simResult.Results) > 0 && len(simResult.Results[0].Auth) > 0 {
		invokeOp := tx.Operations[0].Body.InvokeHostFunctionOp
		invokeOp.Auth = make([]xdr.SorobanAuthorizationEntry, len(simResult.Results[0].Auth))

		for i, authXDR := range simResult.Results[0].Auth {
			var auth xdr.SorobanAuthorizationEntry
			err = xdr.SafeUnmarshalBase64(authXDR, &auth)
			if err != nil {
				return "", fmt.Errorf("failed to parse auth entry: %w", err)
			}
			invokeOp.Auth[i] = auth
		}
	}

	// Re-encode the updated envelope
	updatedXDR, err := xdr.MarshalBase64(txEnvelope)
	if err != nil {
		return "", fmt.Errorf("failed to encode updated transaction: %w", err)
	}

	return updatedXDR, nil
}

// --- SCVal encoding helpers ---

// EncodeAddress encodes a Stellar address to SCVal.
func EncodeAddress(address string) (xdr.ScVal, error) {
	// Check if it's an account (G...) or contract (C...)
	if len(address) != 56 {
		return xdr.ScVal{}, fmt.Errorf("invalid address length")
	}

	switch address[0] {
	case 'G':
		// Account address
		accountID, err := strkey.Decode(strkey.VersionByteAccountID, address)
		if err != nil {
			return xdr.ScVal{}, fmt.Errorf("invalid account ID: %w", err)
		}
		var pubKey xdr.Uint256
		copy(pubKey[:], accountID)
		scAddress := xdr.ScAddress{
			Type: xdr.ScAddressTypeScAddressTypeAccount,
			AccountId: &xdr.AccountId{
				Type:    xdr.PublicKeyTypePublicKeyTypeEd25519,
				Ed25519: &pubKey,
			},
		}
		return xdr.ScVal{
			Type:    xdr.ScValTypeScvAddress,
			Address: &scAddress,
		}, nil

	case 'C':
		// Contract address
		contractIDBytes, err := strkey.Decode(strkey.VersionByteContract, address)
		if err != nil {
			return xdr.ScVal{}, fmt.Errorf("invalid contract ID: %w", err)
		}
		var contractID xdr.ContractId
		copy(contractID[:], contractIDBytes)
		scAddress := xdr.ScAddress{
			Type:       xdr.ScAddressTypeScAddressTypeContract,
			ContractId: &contractID,
		}
		return xdr.ScVal{
			Type:    xdr.ScValTypeScvAddress,
			Address: &scAddress,
		}, nil

	default:
		return xdr.ScVal{}, fmt.Errorf("unsupported address type")
	}
}

// EncodeI128 encodes an int64 to SCVal I128.
// For simplicity, we only handle values that fit in int64.
func EncodeI128(value int64) xdr.ScVal {
	// I128 is represented as (hi: i64, lo: u64)
	// For positive values that fit in int64, hi=0, lo=value
	// For negative values, we need two's complement
	var hi int64
	var lo uint64

	if value >= 0 {
		hi = 0
		lo = uint64(value)
	} else {
		hi = -1
		lo = uint64(value)
	}

	i128Parts := xdr.Int128Parts{
		Hi: xdr.Int64(hi),
		Lo: xdr.Uint64(lo),
	}

	return xdr.ScVal{
		Type: xdr.ScValTypeScvI128,
		I128: &i128Parts,
	}
}

// EncodeU32 encodes a uint32 to SCVal.
func EncodeU32(value uint32) xdr.ScVal {
	v := xdr.Uint32(value)
	return xdr.ScVal{
		Type: xdr.ScValTypeScvU32,
		U32:  &v,
	}
}

// EncodeSymbol encodes a string to SCVal Symbol.
func EncodeSymbol(s string) xdr.ScVal {
	sym := xdr.ScSymbol(s)
	return xdr.ScVal{
		Type: xdr.ScValTypeScvSymbol,
		Sym:  &sym,
	}
}

// EncodeString encodes a string to SCVal String.
func EncodeString(s string) xdr.ScVal {
	str := xdr.ScString(s)
	return xdr.ScVal{
		Type: xdr.ScValTypeScvString,
		Str:  &str,
	}
}

// EncodeBytes encodes bytes to SCVal Bytes.
func EncodeBytes(b []byte) xdr.ScVal {
	bytes := xdr.ScBytes(b)
	return xdr.ScVal{
		Type:  xdr.ScValTypeScvBytes,
		Bytes: &bytes,
	}
}

// EncodeBool encodes a bool to SCVal.
func EncodeBool(b bool) xdr.ScVal {
	return xdr.ScVal{
		Type: xdr.ScValTypeScvBool,
		B:    &b,
	}
}

// --- SCVal decoding helpers ---

// DecodeI128 decodes an SCVal I128 to int64.
// Returns error if value doesn't fit in int64.
func DecodeI128(val xdr.ScVal) (int64, error) {
	if val.Type != xdr.ScValTypeScvI128 || val.I128 == nil {
		return 0, fmt.Errorf("not an I128 value")
	}

	hi := int64(val.I128.Hi)
	lo := uint64(val.I128.Lo)

	// Check if value fits in int64
	// For positive: hi must be 0 and lo must fit
	// For negative: hi must be -1 (all bits set)
	const maxInt64 = uint64(1<<63 - 1)
	if hi == 0 && lo <= maxInt64 {
		return int64(lo), nil
	}
	if hi == -1 && lo > maxInt64 {
		return int64(lo), nil
	}

	return 0, fmt.Errorf("I128 value too large for int64")
}

// DecodeU32 decodes an SCVal U32.
func DecodeU32(val xdr.ScVal) (uint32, error) {
	if val.Type != xdr.ScValTypeScvU32 || val.U32 == nil {
		return 0, fmt.Errorf("not a U32 value")
	}
	return uint32(*val.U32), nil
}

// DecodeBool decodes an SCVal Bool.
func DecodeBool(val xdr.ScVal) (bool, error) {
	if val.Type != xdr.ScValTypeScvBool || val.B == nil {
		return false, fmt.Errorf("not a Bool value")
	}
	return *val.B, nil
}

// DecodeAddress decodes an SCVal Address to string.
func DecodeAddress(val xdr.ScVal) (string, error) {
	if val.Type != xdr.ScValTypeScvAddress || val.Address == nil {
		return "", fmt.Errorf("not an Address value")
	}

	switch val.Address.Type {
	case xdr.ScAddressTypeScAddressTypeAccount:
		if val.Address.AccountId == nil {
			return "", fmt.Errorf("nil account ID")
		}
		return strkey.Encode(strkey.VersionByteAccountID, val.Address.AccountId.Ed25519[:])

	case xdr.ScAddressTypeScAddressTypeContract:
		if val.Address.ContractId == nil {
			return "", fmt.Errorf("nil contract ID")
		}
		return strkey.Encode(strkey.VersionByteContract, val.Address.ContractId[:])

	default:
		return "", fmt.Errorf("unknown address type")
	}
}

// ParseReturnValue parses the return value from a transaction result.
func ParseReturnValue(returnValueXDR string) (xdr.ScVal, error) {
	decoded, err := base64.StdEncoding.DecodeString(returnValueXDR)
	if err != nil {
		return xdr.ScVal{}, fmt.Errorf("failed to decode base64: %w", err)
	}

	var val xdr.ScVal
	err = val.UnmarshalBinary(decoded)
	if err != nil {
		return xdr.ScVal{}, fmt.Errorf("failed to unmarshal ScVal: %w", err)
	}

	return val, nil
}

// BuildContractDataKey builds a ledger key for contract data.
func BuildContractDataKey(contractAddr string, key xdr.ScVal, durability xdr.ContractDataDurability) (string, error) {
	contractIDBytes, err := strkey.Decode(strkey.VersionByteContract, contractAddr)
	if err != nil {
		return "", fmt.Errorf("invalid contract ID: %w", err)
	}

	var contractID xdr.ContractId
	copy(contractID[:], contractIDBytes)

	ledgerKey := xdr.LedgerKey{
		Type: xdr.LedgerEntryTypeContractData,
		ContractData: &xdr.LedgerKeyContractData{
			Contract: xdr.ScAddress{
				Type:       xdr.ScAddressTypeScAddressTypeContract,
				ContractId: &contractID,
			},
			Key:        key,
			Durability: durability,
		},
	}

	xdrBytes, err := xdr.MarshalBase64(ledgerKey)
	if err != nil {
		return "", fmt.Errorf("failed to encode ledger key: %w", err)
	}

	return xdrBytes, nil
}
