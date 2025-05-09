package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

var messageTypeMap map[uint64]protoreflect.MessageType
var extensionTypeDescriptor protoreflect.ExtensionTypeDescriptor

var fillMessageMap = sync.OnceValue(func() error {
	messageTypeMap = make(map[uint64]protoreflect.MessageType)
	et, err := protoregistry.GlobalTypes.FindExtensionByName("id")
	if err != nil {
		return fmt.Errorf("failed to find id extension: %w", err)
	}
	extensionTypeDescriptor = et.TypeDescriptor()
	protoregistry.GlobalTypes.RangeMessages(func(mt protoreflect.MessageType) bool {
		value := getTypeID(mt.Descriptor())
		if value > 0 {
			messageTypeMap[value] = mt
		}
		return true
	})
	return nil
})

// Given a message descriptor, return the type ID.
func getTypeID(mt protoreflect.MessageDescriptor) uint64 {
	return mt.Options().ProtoReflect().Get(extensionTypeDescriptor).Uint()
}

// Do a blocking read of a single varint from the conn, returning the value.
func (c *server) readVarInt() (uint64, error) {
	for {
		v, n := protowire.ConsumeVarint(c.buffer)
		if n >= 0 {
			c.buffer = c.buffer[n:]
			return v, nil
		}
		err := protowire.ParseError(n)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, err
		}
		buf := make([]byte, 10)
		n, err = io.ReadAtLeast(c.conn, buf, 1)
		c.buffer = append(c.buffer, buf[:n]...)
		if err != nil {
			if n <= 0 || !errors.Is(err, io.EOF) {
				return 0, err
			}
		}
	}
}

// Do a blocking read of a message packet, returning the message.
func (c *server) readMessage(ctx context.Context) (proto.Message, error) {
	if err := fillMessageMap(); err != nil {
		return nil, err
	}
	header, err := c.readVarInt()
	if err != nil {
		return nil, fmt.Errorf("failed to read header byte: %w", err)
	}
	if header != 0 {
		return nil, fmt.Errorf("read invalid header byte: %x", header)
	}
	messageSize, err := c.readVarInt()
	if err != nil {
		return nil, fmt.Errorf("failed to read message size: %w", err)
	}
	messageTypeIndex, err := c.readVarInt()
	if err != nil {
		return nil, fmt.Errorf("failed to read message type: %w", err)
	}
	messageType, ok := messageTypeMap[messageTypeIndex]
	if !ok {
		return nil, fmt.Errorf("failed to map message type %d", messageTypeIndex)
	}

	for uint64(len(c.buffer)) < messageSize {
		buf := make([]byte, messageSize-uint64(len(c.buffer)))
		n, err := io.ReadFull(c.conn, buf)
		c.buffer = append(c.buffer, buf[:n]...)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("failed to read message: %w", err)
		}
	}

	message := messageType.New().Interface()
	if err := proto.Unmarshal(c.buffer[:messageSize], message); err != nil {
		name := messageType.Descriptor().FullName()
		slog.ErrorContext(ctx, "failed to unmarshal", "name", name, "buffer", fmt.Sprintf("%+v", c.buffer), "size", messageSize)
		return nil, fmt.Errorf("failed to unmarshal %s message: %w", name, err)
	}
	c.buffer = c.buffer[messageSize:]

	slog.DebugContext(ctx, "received incoming message", "message", message, "type", messageType.Descriptor().FullName())
	return message, nil
}

// Send a message over the wire synchronously.
func (s *server) sendMessage(msg proto.Message) error {
	payload, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal outgoing message: %w", err)
	}
	typeID := getTypeID(msg.ProtoReflect().Descriptor())
	var buf []byte
	buf = protowire.AppendVarint(buf, 0)
	buf = protowire.AppendVarint(buf, uint64(len(payload)))
	buf = protowire.AppendVarint(buf, typeID)
	buf = append(buf, payload...)
	if _, err := s.conn.Write(buf); err != nil {
		return fmt.Errorf("failed to write outgoing message: %w", err)
	}
	return nil
}
