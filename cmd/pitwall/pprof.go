package main

import (
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof on the default mux, served only when PITWALL_PPROF is set
	"os"
)

// startPprof serves Go profiles on a loopback address for performance work, e.g. PITWALL_PPROF=127.0.0.1:6060.
func startPprof() {
	addr := os.Getenv("PITWALL_PPROF")
	if addr == "" {
		return
	}
	go func() { _ = http.ListenAndServe(addr, nil) }()
}
