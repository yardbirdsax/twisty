package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	httppprof "net/http/pprof"
	"time"
)

const defaultPprofAddr = "127.0.0.1:6060"

// startPprof serves the Go pprof endpoints on a loopback address so a running
// process can be profiled without having planned for it:
//
//	go tool pprof http://127.0.0.1:6060/debug/pprof/profile?seconds=15
//	curl http://127.0.0.1:6060/debug/pprof/goroutine?debug=2
//
// If addr is busy (e.g. another twisty process already holds it) it falls back
// to a free loopback port rather than failing. addr "off" disables the server.
// Non-loopback addresses are rejected: pprof exposes process internals.
// It returns the address actually bound ("" when disabled) and a stop func.
func startPprof(addr string, stderr io.Writer) (string, func(), error) {
	noop := func() {}
	if addr == "off" {
		return "", noop, nil
	}
	if err := requireLoopback(addr); err != nil {
		return "", noop, err
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(stderr, "pprof: %s unavailable (%v); using a free loopback port\n", addr, err)
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", noop, fmt.Errorf("pprof: listen: %w", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", httppprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", httppprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", httppprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", httppprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", httppprof.Trace)

	// No write timeout: /debug/pprof/profile?seconds=N legitimately streams for N seconds.
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)

	bound := ln.Addr().String()
	fmt.Fprintf(stderr, "pprof listening on http://%s/debug/pprof/\n", bound)
	return bound, func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}, nil
}

func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid pprof address %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("pprof address %q must be a loopback address (e.g. 127.0.0.1:6060)", addr)
}
