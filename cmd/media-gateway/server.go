package main

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// shutdownTimeout fits inside the reference systemd TimeoutStopSec=30s.
const shutdownTimeout = 10 * time.Second

// newServer applies finite HTTP bounds to the supplied public handler.
func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      deliveryTimeout + 5*time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		// net/http diagnostics may contain attacker-controlled request/connection data.
		// Streaming aborts use http.ErrAbortHandler; lifecycle errors are
		// reported separately through slog, without a per-connection log-flood path.
		ErrorLog: log.New(io.Discard, "", 0),
	}
}

// serve accepts only the listener value returned by config.Load in run.
func serve(ctx context.Context, address string, handler http.Handler, logger *slog.Logger) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if err != nil {
		if ctx.Err() != nil {
			logger.Info("stopped")
			return nil
		}
		return errors.New("cannot bind configured loopback listener")
	}
	return serveListener(ctx, newServer(handler), listener, logger, shutdownTimeout)
}

// serveListener owns the listener and waits for both Serve and bounded shutdown.
func serveListener(ctx context.Context, server *http.Server, listener net.Listener, logger *slog.Logger, timeout time.Duration) error {
	defer listener.Close()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	logger.Info("listening", "listen", listener.Addr().String())
	select {
	case <-done:
		_ = server.Close()
		return errors.New("HTTP server stopped unexpectedly")
	case <-ctx.Done():
		logger.Info("stopping")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := server.Shutdown(shutdownCtx)
	if err != nil {
		// Preserve the shutdown failure even if forced connection cleanup also fails.
		_ = server.Close()
	}
	serveErr := <-done
	if err != nil {
		return errors.New("HTTP graceful shutdown failed; connections closed")
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return errors.New("HTTP server failed during shutdown")
	}
	logger.Info("stopped")
	return nil
}
