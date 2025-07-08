// Package gaia 注释
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

var DefaultTimeOut = time.Second * 30

type HttpServer struct {
	server   *http.Server
	TimeOut  time.Duration
	Addr     string
	CertFile string
	KeyFile  string
}

func NewHttpServer(addr string, handler http.Handler) *HttpServer {
	return &HttpServer{
		server:  &http.Server{Addr: addr, Handler: handler},
		Addr:    addr,
		TimeOut: DefaultTimeOut,
	}
}

func (h *HttpServer) WithTimeOut(t time.Duration) *HttpServer {
	if t < 0 {
		h.TimeOut = DefaultTimeOut
	}
	h.TimeOut = t
	return h
}

func (h *HttpServer) WithCertFile(certFile string) *HttpServer {
	h.CertFile = certFile
	return h
}

func (h *HttpServer) WithKeyFile(keyFile string) *HttpServer {
	h.KeyFile = keyFile
	return h
}

func (h *HttpServer) Start() {
	exitCh := make(chan os.Signal, 1)
	signal.Notify(exitCh, syscall.SIGINT, syscall.SIGTERM)

	if len(h.CertFile) == 0 || len(h.KeyFile) == 0 {
		go func() {
			if err := h.http(); err != nil {
				ErrorF("ListenAndServe Err:%s", err.Error())
				os.Exit(1)
			}
		}()
	} else {
		go func() {
			if err := h.https(); err != nil {
				ErrorF("ListenAndServeTLS Err:%s", err.Error())
				os.Exit(1)
			}
		}()
	}

	call := <-exitCh
	InfoF("Receive exit syscall %v. After %.2fs , server will shutdown ", call.String(), h.TimeOut.Seconds())
	ctx, cancelFunc := context.WithTimeout(context.Background(), h.TimeOut)
	defer cancelFunc()

	if err := h.server.Shutdown(ctx); err != nil {
		ErrorF("Shutdown Err:%s", err.Error())
	} else {
		InfoF("Shutting down server gracefully")
	}
}

func (h *HttpServer) http() error {
	InfoF("HTTP Server Start On %s", h.Addr)
	if err := h.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (h *HttpServer) https() error {
	InfoF("HTTPS Server Start On %s", h.Addr)
	if err := h.server.ListenAndServeTLS(h.CertFile, h.KeyFile); err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
