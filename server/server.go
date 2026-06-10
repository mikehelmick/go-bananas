// Copyright 2026 the go-bananas authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package server provides a small, gracefully-stoppable HTTP server.
//
// A [Server] binds its listener at construction time (so the bound address is
// immediately available via [Server.Addr]), but does not begin serving until
// [Server.ServeHTTP] or [Server.ServeHTTPHandler] is called. Those methods block
// until the provided context is cancelled, at which point the server is shut
// down with a short grace period.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/mikehelmick/go-bananas/logging"
)

// Server is a gracefully-stoppable HTTP server. It is safe for concurrent use.
type Server struct {
	ip       string
	port     string
	listener net.Listener
}

// New creates a server listening on the provided port. The listener is bound
// immediately, so the server is ready to accept connections before serving
// begins. An empty port lets the operating system choose a free one.
func New(port string) (*Server, error) {
	addr := ":" + port
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to create listener on %s: %w", addr, err)
	}

	return NewFromListener(listener)
}

// NewFromListener creates a server on the given listener, useful for customizing
// the listener type or bind options. The listener must be TCP.
func NewFromListener(listener net.Listener) (*Server, error) {
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return nil, fmt.Errorf("listener is not tcp")
	}

	return &Server{
		ip:       addr.IP.String(),
		port:     strconv.Itoa(addr.Port),
		listener: listener,
	}, nil
}

// ServeHTTPHandler is a convenience wrapper around [Server.ServeHTTP] that builds
// an [http.Server] from handler with sensible timeouts. Observability or tracing
// wrappers, if desired, are the caller's responsibility (wrap handler before
// passing it in).
func (s *Server) ServeHTTPHandler(ctx context.Context, handler http.Handler) error {
	return s.ServeHTTP(ctx, &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler:           handler,
	})
}

// ServeHTTP starts the server with the provided [http.Server] and blocks until
// ctx is cancelled, after which it gracefully shuts down with a 5-second
// timeout. A server is not safe to reuse after it has stopped.
func (s *Server) ServeHTTP(ctx context.Context, srv *http.Server) error {
	logger := logging.FromContext(ctx)

	// Shut down when the context is cancelled. The goroutine also exits if Serve
	// returns early (via serveDone), so it never leaks past this call.
	errCh := make(chan error, 1)
	serveDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-serveDone:
			// Serve already returned; nothing to shut down.
			errCh <- nil
			return
		}

		logger.Debug("server.Serve: context closed, shutting down")
		shutdownCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()

		errCh <- srv.Shutdown(shutdownCtx)
	}()

	// Serve blocks until the server is shut down.
	serveErr := srv.Serve(s.listener)
	close(serveDone)
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		<-errCh // let the goroutine finish
		return fmt.Errorf("failed to serve: %w", serveErr)
	}

	logger.Debug("server.Serve: serving stopped")

	// Surface any error that occurred during shutdown.
	if err := <-errCh; err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}
	return nil
}

// Addr returns the server's listening address (host:port).
func (s *Server) Addr() string {
	return net.JoinHostPort(s.ip, s.port)
}

// IP returns the server's listening IP.
func (s *Server) IP() string {
	return s.ip
}

// Port returns the server's listening port.
func (s *Server) Port() string {
	return s.port
}
