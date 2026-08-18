package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	nodeReadHeaderTimeout = 5 * time.Second
	nodeReadTimeout       = 15 * time.Second
	nodeWriteTimeout      = 30 * time.Second
	nodeIdleTimeout       = 60 * time.Second
	nodeShutdownTimeout   = 10 * time.Second
	nodeMaxHeaderBytes    = 1 << 20
)

func newNodeHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: nodeReadHeaderTimeout,
		ReadTimeout:       nodeReadTimeout,
		WriteTimeout:      nodeWriteTimeout,
		IdleTimeout:       nodeIdleTimeout,
		MaxHeaderBytes:    nodeMaxHeaderBytes,
	}
}

func runNodeHTTPServer(addr string, handler http.Handler) error {
	server := newNodeHTTPServer(addr, handler)
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signals:
		ctx, cancel := context.WithTimeout(context.Background(), nodeShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			_ = server.Close()
			return err
		}

		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
