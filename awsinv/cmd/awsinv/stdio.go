package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
)

// runStdio serves the same JSON-RPC protocol as -serve, but reads requests
// from stdin and writes responses to stdout (logs stay on stderr). Closing
// stdin is the shutdown signal clients should use: Windows has no SIGTERM,
// and killing a process there runs no handler. It cancels a scan in
// progress, which still replies with what it finished, then the process
// exits 0. SIGINT/SIGTERM do the same where they exist.
func runStdio() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go cancelOnSignal(cancel)

	s, closeDB := newServer(ctx)
	defer closeDB()

	log.Print("serving on stdin/stdout")
	s.serveStdio(ctx, os.Stdin, os.Stdout)
	log.Print("server stopped")
}

// serveStdio answers requests read from r on w, one at a time, until r ends
// or ctx is cancelled.
//
// r is read in its own goroutine, so EOF is seen while a call runs and
// cancels it; the call's reply is still written. stdin usually can't take a
// read deadline, so on ctx cancel a blocked read is left behind rather than
// interrupted.
func (s *server) serveStdio(ctx context.Context, r io.Reader, w io.Writer) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	lines := make(chan []byte) // unbuffered: EOF is only seen once every line was taken
	go func() {
		defer cancel()
		defer close(lines)
		sc := newLineScanner(r)
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 {
				continue
			}
			select {
			case lines <- bytes.Clone(line):
			case <-ctx.Done():
				return
			}
		}
		if err := sc.Err(); err != nil && ctx.Err() == nil {
			log.Printf("read request: %v", err)
		}
	}()

	enc := json.NewEncoder(w)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return
			}
			if err := s.reply(ctx, enc, line); err != nil {
				log.Printf("write response: %v", err)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
