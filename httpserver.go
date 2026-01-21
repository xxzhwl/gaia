// Package gaia provides a simple HTTP/HTTPS server wrapper
// @author wanlizhan
// @created 2024/4/29
package gaia

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var DefaultShutdownTimeout = time.Second * 30
var DefaultReadTimeout = time.Second * 30
var DefaultWriteTimeout = time.Second * 30
var DefaultIdleTimeout = time.Second * 60

type HttpServer struct {
	server          *http.Server
	shutdownTimeout time.Duration
	certFile        string
	keyFile         string
}

// NewHttpServer creates a new HTTP server instance
func NewHttpServer(addr string, handler http.Handler) *HttpServer {
	return &HttpServer{
		server: &http.Server{
			Addr:         addr,
			Handler:      handler,
			ReadTimeout:  DefaultReadTimeout,
			WriteTimeout: DefaultWriteTimeout,
			IdleTimeout:  DefaultIdleTimeout,
		},
		shutdownTimeout: DefaultShutdownTimeout,
	}
}

// WithShutdownTimeout sets the timeout for graceful shutdown
func (h *HttpServer) WithShutdownTimeout(t time.Duration) *HttpServer {
	if t < 0 {
		h.shutdownTimeout = DefaultShutdownTimeout
	} else {
		h.shutdownTimeout = t
	}
	return h
}

// WithReadTimeout sets the read timeout for the server
func (h *HttpServer) WithReadTimeout(t time.Duration) *HttpServer {
	if t > 0 {
		h.server.ReadTimeout = t
	}
	return h
}

// WithWriteTimeout sets the write timeout for the server
func (h *HttpServer) WithWriteTimeout(t time.Duration) *HttpServer {
	if t > 0 {
		h.server.WriteTimeout = t
	}
	return h
}

// WithIdleTimeout sets the idle timeout for the server
func (h *HttpServer) WithIdleTimeout(t time.Duration) *HttpServer {
	if t > 0 {
		h.server.IdleTimeout = t
	}
	return h
}

// WithCertFile sets the certificate file path for HTTPS
func (h *HttpServer) WithCertFile(certFile string) *HttpServer {
	h.certFile = certFile
	return h
}

// WithKeyFile sets the private key file path for HTTPS
func (h *HttpServer) WithKeyFile(keyFile string) *HttpServer {
	h.keyFile = keyFile
	return h
}

// Start starts the HTTP/HTTPS server and blocks until shutdown
// It handles SIGINT and SIGTERM signals for graceful shutdown
func (h *HttpServer) Start() error {
	exitCh := make(chan os.Signal, 1)
	signal.Notify(exitCh, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	serverErrCh := make(chan error, 1)
	go func() {
		var err error
		if len(h.certFile) == 0 || len(h.keyFile) == 0 {
			err = h.http()
		} else {
			err = h.https()
		}
		serverErrCh <- err
	}()

	// Wait for either server error or shutdown signal
	select {
	case err := <-serverErrCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			ErrorF("Server error: %s", err.Error())
			return err
		}
		return nil
	case sig := <-exitCh:
		InfoF("Received shutdown signal: %v. Starting graceful shutdown with %.2fs timeout", sig.String(), h.shutdownTimeout.Seconds())
		ctx, cancel := context.WithTimeout(context.Background(), h.shutdownTimeout)
		defer cancel()

		if err := h.Shutdown(ctx); err != nil {
			ErrorF("Shutdown error: %s", err.Error())
			return err
		}
		InfoF("Server shutdown gracefully")
		return nil
	}
}

// StartWithContext starts the server with a custom context for cancellation
func (h *HttpServer) StartWithContext(ctx context.Context) error {
	exitCh := make(chan os.Signal, 1)
	signal.Notify(exitCh, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	serverErrCh := make(chan error, 1)
	go func() {
		var err error
		if len(h.certFile) == 0 || len(h.keyFile) == 0 {
			err = h.http()
		} else {
			err = h.https()
		}
		serverErrCh <- err
	}()

	// Wait for either server error, shutdown signal, or context cancellation
	select {
	case err := <-serverErrCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			ErrorF("Server error: %s", err.Error())
			return err
		}
		return nil
	case sig := <-exitCh:
		InfoF("Received shutdown signal: %v. Starting graceful shutdown with %.2fs timeout", sig.String(), h.shutdownTimeout.Seconds())
		return h.Shutdown(context.Background())
	case <-ctx.Done():
		InfoF("Context cancelled. Starting graceful shutdown with %.2fs timeout", h.shutdownTimeout.Seconds())
		return h.Shutdown(context.Background())
	}
}

// Shutdown gracefully shuts down the server with the given context
func (h *HttpServer) Shutdown(ctx context.Context) error {
	// 先停止日志服务，确保所有日志都被刷新
	if logger := GetLogger(); logger != nil {
		logger.Stop()
	}

	// 然后优雅关闭HTTP服务器
	if err := h.server.Shutdown(ctx); err != nil {
		ErrorF("Shutdown error: %s", err.Error())
		return err
	}
	return nil
}

func (h *HttpServer) http() error {
	InfoF("HTTP Server started on %s", h.server.Addr)
	if err := h.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (h *HttpServer) https() error {
	InfoF("HTTPS Server started on %s", h.server.Addr)
	if err := h.server.ListenAndServeTLS(h.certFile, h.keyFile); err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
