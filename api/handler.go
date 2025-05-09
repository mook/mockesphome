package api

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime/debug"

	"github.com/mook/mockesphome/api/pb"
	"google.golang.org/protobuf/proto"
)

var (
	sourceURL = ""
)

// Handler for a HelloRequest
func (c *component) handleHello(ctx context.Context, msg proto.Message, send MessageSender) error {
	if _, ok := msg.(*pb.HelloRequest); !ok {
		return fmt.Errorf("message is not a HelloRequest")
	}
	server, ok := ctx.Value(contextKeyServer).(*server)
	if !ok {
		return fmt.Errorf("failed to get server for message")
	}
	if server.state != connectionStateInitial {
		slog.ErrorContext(ctx, "duplicate HelloRequest", "state", server.state)
		return nil
	}
	resp := &pb.HelloResponse{}
	if hostname, err := os.Hostname(); err == nil {
		resp.SetName(hostname)
	} else {
		slog.ErrorContext(ctx, "error getting host name", "error", err)
		resp.SetName("unknown")
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		resp.SetServerInfo(fmt.Sprintf("%s@%s", info.Main.Path, info.Main.Version))
	} else {
		resp.SetServerInfo("home-assistant-bluetooth-proxy")
	}
	// We need at least API version 1.9 to use bluetooth feature flags
	resp.SetApiVersionMajor(1)
	resp.SetApiVersionMinor(10)
	if err := send(resp); err != nil {
		return err
	}
	server.state = connectionStateSetUp
	return nil
}

// Handler for a ConnectRequest, doing authentication.
func (c *component) handleConnect(ctx context.Context, msg proto.Message, send MessageSender) error {
	req, ok := msg.(*pb.ConnectRequest)
	if !ok {
		return fmt.Errorf("message is not a ConnectRequest")
	}
	s, ok := ctx.Value(contextKeyServer).(*server)
	if !ok {
		return fmt.Errorf("failed to get server for message")
	}
	if s.state != connectionStateSetUp {
		slog.ErrorContext(ctx, "ConnectRequest at invalid state", "state", s.state)
		return nil
	}
	expectedPassword := s.component.config.Password
	invalidPassword := expectedPassword != "" && expectedPassword != req.GetPassword()
	if !invalidPassword {
		s.state = connectionStateAuthed
	}
	response := &pb.ConnectResponse{}
	response.SetInvalidPassword(invalidPassword)
	return send(response)
}

func (c *component) handleDisconnect(ctx context.Context, msg proto.Message, send MessageSender) error {
	if _, ok := msg.(*pb.DisconnectRequest); !ok {
		return fmt.Errorf("message is not a DisconnectRequest")
	}
	s, ok := ctx.Value(contextKeyServer).(*server)
	if !ok {
		return fmt.Errorf("failed to get server for message")
	}
	if err := send(&pb.DisconnectResponse{}); err != nil {
		return err
	}
	s.cancel()
	return nil
}

func (c *component) handleDisconnectResponse(ctx context.Context, msg proto.Message, _ MessageSender) error {
	if _, ok := msg.(*pb.DisconnectRequest); !ok {
		return fmt.Errorf("message is not a DisconnectRequest")
	}
	s, ok := ctx.Value(contextKeyServer).(*server)
	if !ok {
		return fmt.Errorf("failed to get server for message")
	}
	s.cancel()
	return nil
}

// Handler for a DeviceInfoRequest
func (c *component) handleDeviceInfo(ctx context.Context, msg proto.Message, send MessageSender) error {
	if _, ok := msg.(*pb.DeviceInfoRequest); !ok {
		return fmt.Errorf("message is not a DeviceInfoRequest")
	}
	resp := &pb.DeviceInfoResponse{}
	resp.SetManufacturer(sourceURL)
	resp.SetUsesPassword(c.config.Password != "")
	if hostname, err := os.Hostname(); err == nil {
		resp.SetName(hostname)
		resp.SetFriendlyName(hostname)
	} else {
		resp.SetName("unknown")
	}

	// Get the mac addr
	listenerAddr := c.listener.Addr().String()
	interfaces, err := net.Interfaces()
	if err != nil {
		slog.ErrorContext(ctx, "failed to enumerate interfaces", "error", err)
	} else {
		found := false
		fallbackAddr := "(unknown)"
	ifaceLoop:
		for _, iface := range interfaces {
			ifaceAddrs, err := iface.Addrs()
			if err != nil {
				slog.ErrorContext(
					ctx, "failed to get addresses for interface",
					"interface", iface.Name,
					"error", err)
				continue
			}
			for _, ifaceAddr := range ifaceAddrs {
				if ifaceAddr.String() == listenerAddr {
					resp.SetMacAddress(iface.HardwareAddr.String())
					found = true
					break ifaceLoop
				}
			}
			if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 {
				fallbackAddr = iface.HardwareAddr.String()
			}
		}
		if !found {
			resp.SetMacAddress(fallbackAddr)
		}
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		resp.SetEsphomeVersion(info.Main.Version)
		if sourceURL == "" {
			resp.SetManufacturer(info.Main.Path)
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.time" {
				resp.SetCompilationTime(setting.Value)
				break
			}
		}
	}

	for _, handler := range deviceInfoHandlers {
		if err := handler(resp); err != nil {
			slog.ErrorContext(ctx, "failed to call device info handler", "error", err)
		}
	}

	return send(resp)
}

func (c *component) handlePing(ctx context.Context, msg proto.Message, send MessageSender) error {
	if _, ok := msg.(*pb.PingRequest); !ok {
		return fmt.Errorf("message is not a PingRequest")
	}
	return send(&pb.PingResponse{})
}

func (c *component) handleListEntities(ctx context.Context, msg proto.Message, send MessageSender) error {
	if _, ok := msg.(*pb.ListEntitiesRequest); !ok {
		return fmt.Errorf("message is not a ListEntitiesRequest")
	}
	// TODO: Actually have entities to send?
	return send(&pb.ListEntitiesDoneResponse{})
}
