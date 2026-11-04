package main

import (
	"context"
	"io"
	"log"
	"os"
	"sync"
)

// runStdio serves the same JSON-RPC protocol as -serve, but reads requests
// from stdin and writes responses to stdout (logs stay on stderr). It
// returns when stdin hits EOF, or on SIGINT/SIGTERM once an in-flight call
// has delivered its (partial) result.
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

// serveStdio runs serveLines on r/w until r ends or ctx is cancelled.
//
// Unlike a socket, stdin usually can't take a read deadline, so a blocked
// read can't be interrupted. On cancel we instead wait for the in-flight
// call (if any) to write its reply, then return and leave the reader
// goroutine behind; busy stays locked so it never writes again.
func (s *server) serveStdio(ctx context.Context, r io.Reader, w io.Writer) {
	var busy sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.serveLines(ctx, r, w, &busy)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		busy.Lock()
	}
}
