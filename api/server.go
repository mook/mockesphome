package api

import (
	"context"
	"io"
	"log/slog"
	"net"

	"github.com/mook/mockesphome/api/pb"
	"github.com/mook/mockesphome/utils"
	"google.golang.org/protobuf/proto"
)

type connectionState int

const (
	connectionStateDisconnected = connectionState(iota)
	connectionStateInitial
	connectionStateSetUp
	connectionStateAuthed
)

type contextKeyServerType struct{}

var contextKeyServer = contextKeyServerType{}

type server struct {
	id        int
	ctx       context.Context
	state     connectionState
	component *component
	conn      io.ReadWriter      // Underlying connection to send data on
	peer      string             // Description of the remote
	buffer    []byte             // Buffer for partial bytes for the next message to read
	incoming  chan proto.Message // Incoming messages to be processed
	outgoing  chan proto.Message // Outgoing messages yet to be sent out
	cancel    context.CancelFunc // Trigger to close the connection
}

// The main loop for this connection
func (s *server) loop() {
	for {
		select {
		case <-s.ctx.Done():
			s.component.serverLock.Lock()
			delete(s.component.servers, s.id)
			s.component.serverLock.Unlock()
			slog.InfoContext(s.ctx, "closing server due to context cancellation", "peer", s.peer)
			if closer, ok := s.conn.(io.Closer); ok {
				if err := closer.Close(); err != nil {
					slog.DebugContext(s.ctx, "failed to close connection", "error", err)
				}
			}
			if err := s.sendMessage(&pb.DisconnectRequest{}); !utils.AnyError(err, nil, net.ErrClosed) {
				slog.ErrorContext(s.ctx, "failed to disconnect", "error", err)
			}
			s.state = connectionStateDisconnected
			return
		case msg := <-s.outgoing:
			if err := s.sendMessage(msg); err != nil {
				slog.ErrorContext(s.ctx, "failed to send message", "error", err)
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
					s.ctx, "message received in unsupported state",
					"type", descriptor.FullName(),
					"current state", s.state,
					"required state", requiredState)
				continue
			}
			id := getTypeID(descriptor)
			handler := dispatchTable[id]
			if handler == nil {
				slog.WarnContext(
					s.ctx, "no handler found for message",
					"message", msg,
					"type", descriptor.FullName())
			} else if err := handler(s.ctx, msg, s.component.sendMessage); err != nil {
				slog.ErrorContext(
					s.ctx, "failed to handle message",
					"message", msg,
					"error", err,
					"type", descriptor.FullName())
			}
		}
	}
}

func (s *server) listen() {
	for {
		msg, err := s.readMessage()
		if err == nil {
			s.incoming <- msg
		} else {
			if !utils.AnyError(err, io.EOF, net.ErrClosed) {
				slog.ErrorContext(s.ctx, "failed to read message", "error", err)
			}
			s.cancel()
			break
		}
	}
}

// Serve a single connection.
func serve(ctx context.Context, conn net.Conn, component *component) {
	slog.InfoContext(ctx, "starting new connection", "peer", conn.RemoteAddr())
	ctx, cancel := context.WithCancel(ctx)
	server := &server{
		state:     connectionStateInitial,
		component: component,
		conn:      conn,
		peer:      conn.RemoteAddr().String(),
		incoming:  make(chan proto.Message, 10),
		outgoing:  make(chan proto.Message, 10),
		cancel:    cancel,
	}
	ctx = context.WithValue(ctx, contextKeyServer, server)
	server.ctx = ctx

	component.serverLock.Lock()
	server.id = component.serverID
	component.servers[component.serverID] = server
	component.serverID++
	component.serverLock.Unlock()

	go server.listen()
	server.loop()
}
