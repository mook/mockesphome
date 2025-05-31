// The `pprof` component provides an HTTP server to expose profiling information
// over HTTP; see https://pkg.go.dev/net/http/pprof for details.
package pprof

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof"

	"github.com/mook/mockesphome/components"
	"github.com/mook/mockesphome/utils"
)

const (
	defaultPort = 6060
)

// Configuration for this component.
type Configuration struct {
	Port int // Port to listen on; defaults to 6060.
}

type component struct {
	config Configuration
}

func (c *component) Configure(ctx context.Context, load func(any) error) error {
	c.config.Port = defaultPort
	return load(&c.config)
}

func (c *component) Dependencies() []string {
	return nil
}

func (c *component) ID() string {
	return "pprof"
}

func (c *component) Start(ctx context.Context) error {
	listenAddr := fmt.Sprintf(":%d", c.config.Port)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", listenAddr, err)
	}
	server := http.Server{Addr: listener.Addr().String()}
	go func() {
		if err := server.Serve(listener); !utils.AnyError(err, http.ErrServerClosed) {
			slog.ErrorContext(ctx, "error serving", "address", listenAddr, "error", err)
		}
	}()
	slog.InfoContext(ctx, "pprof server started", "address", listener.Addr())
	return nil
}

func init() {
	components.Register(&component{})
}
