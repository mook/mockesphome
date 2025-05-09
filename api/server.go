package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"

	"github.com/mook/mockesphome/api/pb"
	"github.com/mook/mockesphome/utils"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

type connectionState int

const (
	connectionStateInitial = connectionState(iota)
	connectionStateSetUp
	connectionStateAuthed
)

type contextKeyServerType struct{}

var contextKeyServer = contextKeyServerType{}

type server struct {
	state     connectionState
	component *component
	conn      io.ReadWriter      // Underlying connection to send data on
	buffer    []byte             // Buffer for partial bytes for the next message to read
	incoming  chan proto.Message // Incoming messages to be processed
	outgoing  chan proto.Message // Outgoing messages yet to be sent out
	cancel    context.CancelFunc // Trigger to close the connection
}

// The main loop for this connection
func (s *server) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			err := s.sendMessage(&pb.DisconnectRequest{})
			if !utils.AnyError(err, nil, net.ErrClosed, unix.EPIPE, unix.ECONNRESET) {
				slog.ErrorContext(ctx, "failed to disconnect", "error", err)
			}
			return
		case msg := <-s.outgoing:
			if err := s.sendMessage(msg); err != nil {
				slog.ErrorContext(ctx, "failed to send message", "error", err)
			}
		case msg := <-s.incoming:
			descriptor := msg.ProtoReflect().Descriptor()
			requiredState := connectionStateAuthed
			switch msg.(type) {
			case *pb.HelloRequest, *pb.ConnectRequest, *pb.DisconnectRequest, *pb.PingRequest:
				requiredState = connectionStateInitial
			case *pb.DeviceInfoRequest, *pb.GetTimeRequest:
				requiredState = connectionStateSetUp
			}
			if s.state < requiredState {
				slog.ErrorContext(
					ctx, "message received in unsupported state",
					"type", descriptor.FullName(),
					"current state", s.state,
					"required state", requiredState)
				continue
			}
			id := getTypeID(descriptor)
			handler := dispatchTable[id]
			if handler == nil {
				slog.WarnContext(
					ctx, "no handler found for message",
					"message", msg,
					"type", descriptor.FullName())
			} else if err := handler(ctx, msg, s.sendMessage); err != nil {
				slog.ErrorContext(
					ctx, "failed to handle message",
					"message", msg,
					"error", err,
					"type", descriptor.FullName())
			}
		}
	}
}

func (s *server) listen(ctx context.Context) {
	for {
		msg, err := s.readMessage(ctx)
		if err == nil {
			s.incoming <- msg
		} else if !errors.Is(err, io.EOF) {
			slog.ErrorContext(ctx, "failed to read message", "error", err)
			break
		}
	}
}

// Serve a single connection.
func serve(ctx context.Context, conn net.Conn, component *component) {
	slog.DebugContext(ctx, "starting new connection", "peer", conn.RemoteAddr())
	server := &server{
		component: component,
		conn:      conn,
		incoming:  make(chan proto.Message, 10),
		outgoing:  make(chan proto.Message, 10),
	}
	ctx = context.WithValue(ctx, contextKeyServer, server)
	ctx, cancel := context.WithCancel(ctx)
	server.cancel = cancel

	go server.listen(ctx)
	server.loop(ctx)
}
