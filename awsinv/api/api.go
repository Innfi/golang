// Package api is the wire contract of `awsinv -serve`: JSON-RPC 2.0
// (https://www.jsonrpc.org/specification) over a unix stream socket, one
// JSON value per line. A line holds a single request or a batch (array);
// the reply is one line, or nothing when every request was a notification
// (no "id").
//
//	-> {"jsonrpc":"2.0","id":1,"method":"scan","params":{"accounts":["111111111111"]}}
//	<- {"jsonrpc":"2.0","id":1,"result":{"inventory":{...},"failures":[...]}}
//	<- {"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"..."}}
package api

import (
	"encoding/json"

	"github.com/innfi/awsinv/inventory"
)

// Version is the only value allowed in the "jsonrpc" member.
const Version = "2.0"

// Request is a JSON-RPC request. A nil ID (member absent) makes it a
// notification; send json.RawMessage("null") only if you really mean id null.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

// Response carries exactly one of Result or Error. ID echoes the request's
// id, or is null when the request could not be parsed.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// Error is a JSON-RPC error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Standard codes from the spec, plus implementation-defined server errors
// (-32000..-32099).
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	CodeShuttingDown = -32000 // server got SIGINT/SIGTERM before the call ran
)

// MethodScan collects the inventory. Params: ScanParams (optional).
// Result: ScanResult.
const MethodScan = "scan"

// ScanParams selects accounts to scan. Empty Accounts means every active
// account. An account that is unknown or disabled fails the whole call
// with CodeInvalidParams.
type ScanParams struct {
	Accounts []string `json:"accounts,omitempty"`
}

// ScanResult is the inventory plus per-target failures. A scan cancelled
// by server shutdown or timeout still returns a result: what finished, and
// a failure for each target that didn't.
type ScanResult struct {
	Inventory inventory.Inventory `json:"inventory"`
	Failures  []ScanFailure       `json:"failures"`
}

// ScanFailure is one (resource, account, region) that could not be scanned.
type ScanFailure struct {
	Resource  string `json:"resource"`
	AccountID string `json:"accountId"`
	Region    string `json:"region"`
	Message   string `json:"message"`
}
