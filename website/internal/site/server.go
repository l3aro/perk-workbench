package site

import "net/http"

// New returns the website HTTP handler.
func New(version string) http.Handler {
	_ = version
	return http.NewServeMux()
}
