package main

import (
	"bytes"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestStartPprof_servesProfilesOnLoopback(t *testing.T) {
	var stderr bytes.Buffer

	addr, stop, err := startPprof("127.0.0.1:0", &stderr)
	if err != nil {
		t.Fatalf("startPprof: %v", err)
	}
	defer stop()

	resp, err := http.Get("http://" + addr + "/debug/pprof/goroutine?debug=1")
	if err != nil {
		t.Fatalf("GET goroutine profile: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(stderr.String(), "http://"+addr+"/debug/pprof/") {
		t.Errorf("expected the chosen address to be announced on stderr, got %q", stderr.String())
	}
}

func TestStartPprof_fallsBackToFreePortWhenBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	var stderr bytes.Buffer

	addr, stop, err := startPprof(busy.Addr().String(), &stderr)
	if err != nil {
		t.Fatalf("expected fallback instead of error, got %v", err)
	}
	defer stop()

	if addr == busy.Addr().String() {
		t.Fatalf("expected a different address than the busy one, got %s", addr)
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Errorf("fallback must stay on loopback, got %s", addr)
	}
	resp, err := http.Get("http://" + addr + "/debug/pprof/")
	if err != nil {
		t.Fatalf("fallback server not serving: %v", err)
	}
	resp.Body.Close()
}

func TestStartPprof_offDisablesTheServer(t *testing.T) {
	addr, stop, err := startPprof("off", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("startPprof(off): %v", err)
	}
	defer stop()
	if addr != "" {
		t.Errorf("expected no address when disabled, got %q", addr)
	}
}

func TestBuildCmd_pprofIsOnByDefaultAndOnLoopback(t *testing.T) {
	f := newBuildCmd().Flags().Lookup("pprof-addr")
	if f == nil {
		t.Fatal("expected a --pprof-addr flag on build")
	}
	if f.DefValue == "" || f.DefValue == "off" {
		t.Errorf("pprof must be on by default, got default %q", f.DefValue)
	}
	if err := requireLoopback(f.DefValue); err != nil {
		t.Errorf("default pprof address must be loopback: %v", err)
	}
}

func TestStartPprof_rejectsNonLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:6060", ":6060", "192.168.1.5:6060"} {
		if _, _, err := startPprof(addr, &bytes.Buffer{}); err == nil {
			t.Errorf("expected %q to be rejected: pprof exposes process internals", addr)
		}
	}
}
