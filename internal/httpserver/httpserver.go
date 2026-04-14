package httpserver

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
)

// Config holds HTTP server configuration.
type Config struct {
	Port    int
	TLSCert string
	TLSKey  string
	Logger  *slog.Logger
}

// Server is the HTTP front page server.
type Server struct {
	srv    *http.Server
	config Config
}

// New creates a new HTTP server serving the given filesystem.
func New(fsys fs.FS, cfg Config) *Server {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(fsys)))

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: mux,
	}

	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		srv.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	return &Server{srv: srv, config: cfg}
}

// Start starts the HTTP server in a goroutine.
func (s *Server) Start() {
	logger := s.config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	go func() {
		if s.config.TLSCert != "" && s.config.TLSKey != "" {
			logger.Info("HTTP server starting (TLS)", "port", s.config.Port)
			if err := s.srv.ListenAndServeTLS(s.config.TLSCert, s.config.TLSKey); err != nil && err != http.ErrServerClosed {
				logger.Error("HTTP server error", "error", err)
			}
		} else {
			logger.Info("HTTP server starting", "port", s.config.Port)
			if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("HTTP server error", "error", err)
			}
		}
	}()
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
