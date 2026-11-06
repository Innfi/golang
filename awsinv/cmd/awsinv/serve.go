package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/innfi/awsinv/api"
	"github.com/innfi/awsinv/inventory"
	"github.com/innfi/awsinv/store"
)

// The wire contract (JSON-RPC 2.0, one JSON value per line) lives in package
// api so clients can share it.

type server struct {
	accounts  store.AccountStore
	collector *inventory.Collector
	// scanSlot serializes scans: the Collector's concurrency limits are per
	// Collect call, so parallel requests would multiply AWS API pressure.
	scanSlot chan struct{}
}

// runServer serves scan requests on a unix socket until SIGINT/SIGTERM.
// On signal it stops accepting, cancels in-flight scans (their partial
// results are still sent), waits for connections to finish and removes the
// socket file.
func runServer(path string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go cancelOnSignal(cancel)

	s, closeDB := newServer(ctx)
	defer closeDB()

	ln, err := listenUnix(path)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("listening on %s", path)

	s.serve(ctx, ln)
	log.Print("server stopped")
}

// newServer wires the account store and collector. The returned func
// closes the database.
func newServer(ctx context.Context) (*server, func()) {
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	c, err := newCollector(ctx)
	if err != nil {
		db.Close()
		log.Fatal(err)
	}
	s := &server{
		accounts:  &store.SQLStore{DB: db},
		collector: c,
		scanSlot:  make(chan struct{}, 1),
	}
	return s, func() { db.Close() }
}

// listenUnix binds path, replacing a stale socket file left by a crashed
// process but refusing to steal one a live server still answers on.
func listenUnix(path string) (*net.UnixListener, error) {
	if _, err := os.Stat(path); err == nil {
		if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
			conn.Close()
			return nil, fmt.Errorf("%s: another server is already listening", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale socket: %w", err)
		}
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	ln.SetUnlinkOnClose(true)
	return ln, nil
}

func (s *server) serve(ctx context.Context, ln *net.UnixListener) {
	stop := context.AfterFunc(ctx, func() { ln.Close() })
	defer stop()

	var wg sync.WaitGroup
	defer wg.Wait()
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("accept: %v", err)
			return
		}
		wg.Go(func() { s.handle(ctx, conn) })
	}
}

// maxLine bounds one request line (a batch included).
const maxLine = 1 << 20

func (s *server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	// On shutdown, unblock an idle read but leave writes working so a
	// cancelled scan can still deliver its partial result.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetReadDeadline(time.Now()) })
	defer stop()

	s.serveLines(ctx, conn, conn)
}

// serveLines answers newline-delimited requests read from r on w until r
// ends or fails.
func (s *server) serveLines(ctx context.Context, r io.Reader, w io.Writer) {
	sc := newLineScanner(r)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := s.reply(ctx, enc, line); err != nil {
			log.Printf("write response: %v", err)
			return
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		log.Printf("read request: %v", err)
	}
}

func newLineScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	return sc
}

// reply dispatches one line and writes the reply, if there is one.
func (s *server) reply(ctx context.Context, enc *json.Encoder, line []byte) error {
	reply := s.dispatch(ctx, line)
	if reply == nil { // only notifications, nothing to send
		return nil
	}
	return enc.Encode(reply)
}

// dispatch handles one line: a request or a batch. It returns the value to
// write back, or nil when the spec says to stay silent.
func (s *server) dispatch(ctx context.Context, line []byte) any {
	if !json.Valid(line) {
		return errorResponse(nil, &api.Error{Code: api.CodeParseError, Message: "parse error"})
	}
	if line[0] != '[' {
		if r := s.call(ctx, line); r != nil {
			return r
		}
		return nil
	}

	var batch []json.RawMessage
	if err := json.Unmarshal(line, &batch); err != nil || len(batch) == 0 {
		return errorResponse(nil, &api.Error{Code: api.CodeInvalidRequest, Message: "invalid request: empty batch"})
	}
	var replies []*api.Response
	for _, raw := range batch {
		if r := s.call(ctx, raw); r != nil {
			replies = append(replies, r)
		}
	}
	if len(replies) == 0 {
		return nil
	}
	return replies
}

// call runs one request. It returns nil for a valid notification.
func (s *server) call(ctx context.Context, raw []byte) *api.Response {
	var req api.Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return errorResponse(nil, &api.Error{Code: api.CodeInvalidRequest, Message: "invalid request: " + err.Error()})
	}
	if !validID(req.ID) {
		return errorResponse(nil, &api.Error{Code: api.CodeInvalidRequest, Message: "invalid request: id must be a string, number or null"})
	}
	if req.JSONRPC != api.Version || req.Method == "" {
		return errorResponse(req.ID, &api.Error{Code: api.CodeInvalidRequest, Message: `invalid request: need "jsonrpc":"2.0" and a method`})
	}

	result, rpcErr := s.invoke(ctx, req)
	if req.ID == nil {
		return nil // notification: ran it, but never reply
	}
	if rpcErr != nil {
		return errorResponse(req.ID, rpcErr)
	}
	b, err := json.Marshal(result)
	if err != nil {
		return errorResponse(req.ID, &api.Error{Code: api.CodeInternalError, Message: "encode result: " + err.Error()})
	}
	return &api.Response{JSONRPC: api.Version, Result: b, ID: req.ID}
}

func (s *server) invoke(ctx context.Context, req api.Request) (any, *api.Error) {
	switch req.Method {
	case api.MethodScan:
		var p api.ScanParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "invalid params: " + err.Error()}
		}
		return s.scan(ctx, p)
	case api.MethodAccounts:
		if err := decodeParams(req.Params, &struct{}{}); err != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "invalid params: " + err.Error()}
		}
		return s.listAccounts(ctx)
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "method not found: " + req.Method}
	}
}

// decodeParams accepts absent/null params or an object. Unknown fields are
// rejected so a typo like "acounts" doesn't silently scan everything.
func decodeParams(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] != '{' {
		return errors.New("params must be an object")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// validID: absent (notification), null, string or number.
func validID(id json.RawMessage) bool {
	if id == nil {
		return true
	}
	switch c := id[0]; {
	case c == '"', c == '-', c >= '0' && c <= '9':
		return true
	default:
		return string(id) == "null"
	}
}

func errorResponse(id json.RawMessage, e *api.Error) *api.Response {
	if id == nil {
		id = json.RawMessage("null")
	}
	return &api.Response{JSONRPC: api.Version, Error: e, ID: id}
}

func (s *server) scan(ctx context.Context, p api.ScanParams) (*api.ScanResult, *api.Error) {
	select {
	case s.scanSlot <- struct{}{}:
		defer func() { <-s.scanSlot }()
	case <-ctx.Done():
		return nil, &api.Error{Code: api.CodeShuttingDown, Message: "server shutting down"}
	}

	ctx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()

	accounts, err := loadAccounts(ctx, s.accounts, p.Accounts)
	if errors.Is(err, errUnknownAccount) {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: err.Error()}
	}
	if err != nil {
		log.Printf("load accounts: %v", err)
		return nil, &api.Error{Code: api.CodeInternalError, Message: "load accounts: " + err.Error()}
	}

	inv := s.collector.Collect(ctx, accounts)
	failures := make([]api.ScanFailure, len(inv.Errors))
	for i, e := range inv.Errors {
		log.Printf("scan failed: %v", e)
		failures[i] = api.ScanFailure{Resource: e.Resource, AccountID: e.AccountID, Region: e.Region, Message: e.Err.Error()}
	}
	logSummary(len(accounts), inv)
	return &api.ScanResult{Incomplete: cutShort(ctx, inv.Errors), Inventory: inv, Failures: failures}, nil
}

// cutShort reports whether ctx ending stopped some target. A scan that
// finished just before cancel is still complete.
func cutShort(ctx context.Context, errs []inventory.ScanError) bool {
	if ctx.Err() == nil {
		return false
	}
	return slices.ContainsFunc(errs, func(e inventory.ScanError) bool {
		return errors.Is(e.Err, context.Canceled) || errors.Is(e.Err, context.DeadlineExceeded)
	})
}

// accountsTimeout bounds the accounts query. It does not follow server
// shutdown: the call is short, and a client that sends it and then closes
// stdin still expects the answer.
const accountsTimeout = 30 * time.Second

func (s *server) listAccounts(ctx context.Context) (*api.AccountsResult, *api.Error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), accountsTimeout)
	defer cancel()

	accounts, err := s.accounts.ListActiveAccounts(ctx)
	if err != nil {
		log.Printf("load accounts: %v", err)
		return nil, &api.Error{Code: api.CodeInternalError, Message: "load accounts: " + err.Error()}
	}
	// Field by field: RoleARN and ExternalID must never leave the server.
	out := make([]api.AccountInfo, len(accounts))
	for i, a := range accounts {
		regions := a.Regions
		if len(regions) == 0 {
			regions = s.collector.DefaultRegions
		}
		out[i] = api.AccountInfo{AccountID: a.AccountID, Name: a.Name, Regions: append([]string{}, regions...)}
	}
	return &api.AccountsResult{Accounts: out}, nil
}
