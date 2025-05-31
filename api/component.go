// The `api` component implements the ESPHome native API server; this is required
// to communicate with Home Assistant using the ESPHome protocol.
// At this point only the unencrypted protocol is implemented.
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"os"
	"slices"
	"sync"
	"syscall"

	"github.com/brutella/dnssd"
	"github.com/mook/mockesphome/api/pb"
	"github.com/mook/mockesphome/components"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

const (
	defaultPort = 6053
)

// Configuration for the component.
type Configuration struct {
	Port     int    // The port to listen on; defaults to 6053.
	Password string // Optional password.
}

// ESPHome native API component
type component struct {
	config     Configuration
	listener   net.Listener
	serverID   int
	serverLock sync.Mutex
	servers    map[int]*server
}

func (c *component) ID() string {
	return "api"
}

func (c *component) Dependencies() []string {
	return nil
}

func (c *component) Configure(ctx context.Context, load func(any) error) error {
	handlers := []struct {
		proto.Message
		MessageHandler
	}{
		{&pb.HelloRequest{}, c.handleHello},
		{&pb.ConnectRequest{}, c.handleConnect},
		{&pb.DisconnectRequest{}, c.handleDisconnect},
		{&pb.DisconnectResponse{}, c.handleDisconnectResponse},
		{&pb.DeviceInfoRequest{}, c.handleDeviceInfo},
		{&pb.PingRequest{}, c.handlePing},
		{&pb.ListEntitiesRequest{}, c.handleListEntities},
	}
	for _, handler := range handlers {
		if err := RegisterHandler(handler.Message, handler.MessageHandler); err != nil {
			return err
		}
	}
	return load(&c.config)
}

func (c *component) Start(ctx context.Context) error {
	port := c.config.Port
	if port == 0 {
		port = defaultPort
	}
	listenConfig := &net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			slog.DebugContext(ctx, "controlling conn", "conn", c)
			return c.Control(func(fd uintptr) {
				err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
				if err != nil {
					slog.ErrorContext(ctx, "failed to set SO_REUSEADDR", "error", err, "conn", c)
				}
			})
		},
	}
	listener, err := listenConfig.Listen(ctx, "tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen for connections: %w", err)
	}
	go func() {
		<-ctx.Done()
		slog.DebugContext(ctx, "closing listener")
		if err := listener.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close listener: %w", "error", err)
		}
	}()
	c.listener = listener
	go func() {
		for {
			slog.DebugContext(ctx, "waiting for connection")
			conn, err := listener.Accept()
			if err == nil {
				go serve(ctx, conn, c)
			} else if errors.Is(err, net.ErrClosed) {
				break
			} else {
				slog.ErrorContext(ctx, "failed to accept connection", "error", err)
				break
			}
		}
	}()

	if err := runmDNS(ctx, port); err != nil {
		return err
	}

	slog.InfoContext(ctx, "listening for ESPHome native API", "port", port)
	return nil
}

func (c *component) sendMessage(msg proto.Message) error {
	c.serverLock.Lock()
	servers := slices.Collect(maps.Values(c.servers))
	c.serverLock.Unlock()

	var errs []error

	for _, server := range servers {
		err := server.sendMessage(msg)
		if err != nil {
			slog.ErrorContext(server.ctx, "failed to send message", "error", err)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func runmDNS(ctx context.Context, port int) error {
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get host name: %w", err)
	}
	service, err := dnssd.NewService(dnssd.Config{
		Name: hostname,
		Type: "_esphomelib._tcp",
		Port: port,
	})
	if err != nil {
		return fmt.Errorf("failed to create mDNS service: %w", err)
	}
	responder, err := dnssd.NewResponder()
	if err != nil {
		return fmt.Errorf("failed to create mDNS responder: %w", err)
	}
	_, err = responder.Add(service)
	if err != nil {
		return fmt.Errorf("failed to add service to mDNS responder: %w", err)
	}
	go func() {
		slog.DebugContext(ctx, "starting mDNS responder...")
		if err := responder.Respond(ctx); err != nil {
			slog.ErrorContext(ctx, "failed to run mDNS responder", "error", err)
		}
		slog.DebugContext(ctx, "mDNS stopped")
	}()

	return nil
}

func init() {
	instance := &component{
		servers: make(map[int]*server),
	}
	components.Register(instance)
}
