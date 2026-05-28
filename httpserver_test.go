package gaia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNewHttpServer tests the creation of a new HttpServer instance
func TestNewHttpServer(t *testing.T) {
	addr := ":8080"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := NewHttpServer(addr, handler)
	if server == nil {
		t.Fatal("Expected server instance, got nil")
	}

	if server.server.Addr != addr {
		t.Errorf("Expected server address %s, got %s", addr, server.server.Addr)
	}

	// Test default timeouts
	if server.server.ReadTimeout != DefaultReadTimeout {
		t.Errorf("Expected default read timeout %v, got %v", DefaultReadTimeout, server.server.ReadTimeout)
	}
	if server.server.WriteTimeout != DefaultWriteTimeout {
		t.Errorf("Expected default write timeout %v, got %v", DefaultWriteTimeout, server.server.WriteTimeout)
	}
	if server.server.IdleTimeout != DefaultIdleTimeout {
		t.Errorf("Expected default idle timeout %v, got %v", DefaultIdleTimeout, server.server.IdleTimeout)
	}
}

// TestHttpServer_WithMethods tests the method chaining functionality
func TestHttpServer_WithMethods(t *testing.T) {
	addr := ":8080"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	timeout := 15 * time.Second
	readTimeout := 10 * time.Second
	writeTimeout := 20 * time.Second
	idleTimeout := 45 * time.Second
	certFile := "test.crt"
	keyFile := "test.key"

	server := NewHttpServer(addr, handler).
		WithShutdownTimeout(timeout).
		WithReadTimeout(readTimeout).
		WithWriteTimeout(writeTimeout).
		WithIdleTimeout(idleTimeout).
		WithCertFile(certFile).
		WithKeyFile(keyFile)

	if server.shutdownTimeout != timeout {
		t.Errorf("Expected shutdown timeout %v, got %v", timeout, server.shutdownTimeout)
	}
	if server.server.ReadTimeout != readTimeout {
		t.Errorf("Expected read timeout %v, got %v", readTimeout, server.server.ReadTimeout)
	}
	if server.server.WriteTimeout != writeTimeout {
		t.Errorf("Expected write timeout %v, got %v", writeTimeout, server.server.WriteTimeout)
	}
	if server.server.IdleTimeout != idleTimeout {
		t.Errorf("Expected idle timeout %v, got %v", idleTimeout, server.server.IdleTimeout)
	}
	if server.certFile != certFile {
		t.Errorf("Expected cert file %s, got %s", certFile, server.certFile)
	}
	if server.keyFile != keyFile {
		t.Errorf("Expected key file %s, got %s", keyFile, server.keyFile)
	}
}

// TestHttpServer_WithNegativeTimeout tests that negative timeouts are handled correctly
func TestHttpServer_WithNegativeTimeout(t *testing.T) {
	addr := ":8080"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := NewHttpServer(addr, handler).WithShutdownTimeout(-1 * time.Second)
	if server.shutdownTimeout != DefaultShutdownTimeout {
		t.Errorf("Expected default shutdown timeout for negative input, got %v", server.shutdownTimeout)
	}
}

// TestHttpServer_ServerMethods tests the HTTP and HTTPS server methods
func TestHttpServer_ServerMethods(t *testing.T) {
	addr := ":8080"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Test HTTP method
	server := NewHttpServer(addr, handler)
	// We can't actually start the server in a unit test without blocking,
	// but we can test that the method doesn't panic
	done := make(chan bool)
	go func() {
		// Create a test request
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		server.server.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
		done <- true
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Test timed out")
	}
}

// TestHttpServer_Shutdown tests the Shutdown method
func TestHttpServer_Shutdown(t *testing.T) {
	addr := ":8081"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond) // Simulate slow handler
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := NewHttpServer(addr, handler)

	// Start server in a goroutine
	serverErrCh := make(chan error, 1)
	go func() {
		// Use StartWithContext with a context that we can cancel
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		serverErrCh <- server.StartWithContext(ctx)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Create a context with timeout for shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()

	// Shutdown the server
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Expected no error on shutdown, got %v", err)
	}

	// Check that server exited
	select {
	case err := <-serverErrCh:
		if err != nil {
			t.Errorf("Expected no error from server, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Server did not exit in time")
	}
}

// TestHttpServer_StartWithContext tests the StartWithContext method with context cancellation
func TestHttpServer_StartWithContext(t *testing.T) {
	addr := ":8082"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := NewHttpServer(addr, handler)

	// Create a context that we can cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Start server in a goroutine
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- server.StartWithContext(ctx)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel the context to trigger shutdown
	cancel()

	// Check that server exited gracefully
	select {
	case err := <-serverErrCh:
		if err != nil {
			t.Errorf("Expected no error from server, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Server did not exit in time")
	}
}

// TestHttpServer_WithCustomTimeouts tests the custom timeout settings
func TestHttpServer_WithCustomTimeouts(t *testing.T) {
	addr := ":8083"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Set custom timeouts
	readTimeout := 5 * time.Second
	writeTimeout := 10 * time.Second
	idleTimeout := 30 * time.Second

	server := NewHttpServer(addr, handler).
		WithReadTimeout(readTimeout).
		WithWriteTimeout(writeTimeout).
		WithIdleTimeout(idleTimeout)

	if server.server.ReadTimeout != readTimeout {
		t.Errorf("Expected read timeout %v, got %v", readTimeout, server.server.ReadTimeout)
	}
	if server.server.WriteTimeout != writeTimeout {
		t.Errorf("Expected write timeout %v, got %v", writeTimeout, server.server.WriteTimeout)
	}
	if server.server.IdleTimeout != idleTimeout {
		t.Errorf("Expected idle timeout %v, got %v", idleTimeout, server.server.IdleTimeout)
	}
}
