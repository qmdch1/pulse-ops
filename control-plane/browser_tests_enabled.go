//go:build testseed

package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed testweb
var browserTests embed.FS

func installBrowserTests(mux *http.ServeMux, mode string) {
	if mode != "test" {
		return
	}
	sub, _ := fs.Sub(browserTests, "testweb")
	mux.Handle("GET /__tests__/", http.StripPrefix("/__tests__/", http.FileServer(http.FS(sub))))
}
