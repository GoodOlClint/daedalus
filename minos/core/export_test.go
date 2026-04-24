package core

import "net/http"

// TestingHTTPHandler exposes the private router for tests in core_test.
// The name signals "test-only" per the convention of export_test.go files.
func TestingHTTPHandler(s *Server) http.Handler {
	return s.routes()
}
