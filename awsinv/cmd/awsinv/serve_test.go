package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/innfi/awsinv/api"
	"github.com/innfi/awsinv/inventory"
	"github.com/innfi/awsinv/store"
)

type fakeStore []store.Account

func (f fakeStore) ListActiveAccounts(context.Context) ([]store.Account, error) { return f, nil }

func TestServeRoundTripAndShutdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "awsinv.sock")
	ln, err := listenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	// No accounts => Collect never touches AWS.
	s := &server{accounts: fakeStore{}, collector: &inventory.Collector{}, scanSlot: make(chan struct{}, 1)}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.serve(ctx, ln); close(done) }()

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	send := func(line string) {
		t.Helper()
		if _, err := conn.Write([]byte(line + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	readLine := func() []byte {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		b, err := r.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	call := func(line string) api.Response {
		t.Helper()
		send(line)
		b := readLine()
		var resp api.Response
		if err := json.Unmarshal(b, &resp); err != nil {
			t.Fatalf("decode %q: %v", b, err)
		}
		if resp.JSONRPC != "2.0" {
			t.Fatalf("missing jsonrpc 2.0: %s", b)
		}
		return resp
	}
	wantErr := func(line string, code int, id string) {
		t.Helper()
		resp := call(line)
		if resp.Error == nil || resp.Error.Code != code || string(resp.ID) != id || resp.Result != nil {
			t.Fatalf("%s: got error=%+v id=%s result=%s, want code %d id %s", line, resp.Error, resp.ID, resp.Result, code, id)
		}
	}

	resp := call(`{"jsonrpc":"2.0","id":1,"method":"scan"}`)
	if resp.Error != nil || string(resp.ID) != "1" {
		t.Fatalf("scan all: %+v", resp)
	}
	var res api.ScanResult
	if err := json.Unmarshal(resp.Result, &res); err != nil || res.Failures == nil {
		t.Fatalf("result %s: %v", resp.Result, err)
	}

	wantErr(`not json`, api.CodeParseError, "null")
	wantErr(`{"jsonrpc":"1.0","id":"a","method":"scan"}`, api.CodeInvalidRequest, `"a"`)
	wantErr(`{"jsonrpc":"2.0","id":{},"method":"scan"}`, api.CodeInvalidRequest, "null")
	wantErr(`{"jsonrpc":"2.0","id":2,"method":"nope"}`, api.CodeMethodNotFound, "2")
	wantErr(`{"jsonrpc":"2.0","id":3,"method":"scan","params":{"acounts":[]}}`, api.CodeInvalidParams, "3")
	wantErr(`{"jsonrpc":"2.0","id":4,"method":"scan","params":["x"]}`, api.CodeInvalidParams, "4")
	wantErr(`{"jsonrpc":"2.0","id":5,"method":"scan","params":{"accounts":["999999999999"]}}`, api.CodeInvalidParams, "5")
	wantErr(`[]`, api.CodeInvalidRequest, "null")

	// Notifications get no reply, alone or in a batch; the next reply on the
	// wire must belong to the request after them.
	send(`{"jsonrpc":"2.0","method":"scan"}`)
	send(`[{"jsonrpc":"2.0","method":"scan"},{"jsonrpc":"2.0","method":"nope"}]`)
	send(`[{"jsonrpc":"2.0","id":"x","method":"scan"},{"jsonrpc":"2.0","method":"scan"},{"jsonrpc":"2.0","id":"y","method":"nope"},1]`)
	var batch []api.Response
	if b := readLine(); json.Unmarshal(b, &batch) != nil || len(batch) != 3 {
		t.Fatalf("batch reply: %s", b)
	}
	if string(batch[0].ID) != `"x"` || batch[0].Error != nil ||
		string(batch[1].ID) != `"y"` || batch[1].Error.Code != api.CodeMethodNotFound ||
		string(batch[2].ID) != "null" || batch[2].Error.Code != api.CodeInvalidRequest {
		t.Fatalf("batch reply: %+v", batch)
	}

	if _, err := listenUnix(path); err == nil {
		t.Fatal("second listener should refuse a live socket")
	}

	cancel() // what SIGINT/SIGTERM does
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after cancel (idle connection not released?)")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("socket file not removed: %v", err)
	}
}

func TestServeStdio(t *testing.T) {
	s := &server{accounts: fakeStore{}, collector: &inventory.Collector{}, scanSlot: make(chan struct{}, 1)}

	t.Run("eof", func(t *testing.T) {
		in := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"scan\"}\n\n" +
			"{\"jsonrpc\":\"2.0\",\"method\":\"scan\"}\n" +
			"{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"nope\"}\n")
		var out bytes.Buffer
		s.serveStdio(context.Background(), in, &out)

		lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
		if len(lines) != 2 {
			t.Fatalf("want 2 reply lines, got %q", out.String())
		}
		var r1, r2 api.Response
		if json.Unmarshal([]byte(lines[0]), &r1) != nil || string(r1.ID) != "1" || r1.Error != nil {
			t.Fatalf("reply 1: %s", lines[0])
		}
		if json.Unmarshal([]byte(lines[1]), &r2) != nil || string(r2.ID) != "2" || r2.Error == nil || r2.Error.Code != api.CodeMethodNotFound {
			t.Fatalf("reply 2: %s", lines[1])
		}
	})

	t.Run("cancel while reading", func(t *testing.T) {
		pr, pw := io.Pipe() // never written: read blocks like an idle stdin
		defer pw.Close()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { s.serveStdio(ctx, pr, io.Discard); close(done) }()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("serveStdio did not return after cancel")
		}
	})
}
