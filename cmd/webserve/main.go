//go:build !js

// Command webserve is a tiny static file server for the rp6 web build. It sets
// the cross-origin isolation headers (COOP/COEP + CORP) that browsers require to
// expose SharedArrayBuffer, which rp6's AudioWorklet audio path needs, and the
// correct application/wasm MIME type. Use it via `make serve`.
//
// For hosting elsewhere, serve build/web with these same headers.
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()
	dir := "build/web"
	if flag.NArg() > 0 {
		dir = flag.Arg(0)
	}

	files := http.FileServer(http.Dir(dir))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Cross-origin isolation: required for SharedArrayBuffer (AudioWorklet).
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Embedder-Policy", "require-corp")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if strings.HasSuffix(r.URL.Path, ".wasm") {
			h.Set("Content-Type", "application/wasm")
		}
		files.ServeHTTP(w, r)
	})

	log.Printf("rp6 web: serving %q at http://localhost%s (cross-origin isolated)", dir, *addr)
	log.Fatal(http.ListenAndServe(*addr, handler))
}
