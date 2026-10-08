//go:build !testseed

package main

import "net/http"

func installBrowserTests(_ *http.ServeMux, _ string) {}
