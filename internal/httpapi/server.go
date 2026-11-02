package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

// NewServer returns an http.Server for h with explicit timeouts and limits.
// Errors from the server itself are logged through logger.
func NewServer(h http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}
