package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mook/mockesphome/api/pb"
	"google.golang.org/protobuf/proto"
)

// MessageSender is a function that sends a message to the client.
type MessageSender func(proto.Message) error

// MessageHandler is a function that handles a particular message.  The handlers
// can be registered via [RegisterHandler].
type MessageHandler func(context.Context, proto.Message, MessageSender) error

var dispatchTable = make(map[uint64]MessageHandler)

// Register a message handler.  The sample message should be of the same type
// the handler expects.
func RegisterHandler(sample proto.Message, handler MessageHandler) error {
	if err := fillMessageMap(); err != nil {
		return err
	}
	descriptor := sample.ProtoReflect().Descriptor()
	id := getTypeID(descriptor)
	if id < 1 {
		return fmt.Errorf("failed to find type ID for %s message", descriptor.FullName())
	}
	if _, ok := dispatchTable[id]; ok {
		return fmt.Errorf("message type %s already has a handler", descriptor.FullName())
	}
	dispatchTable[id] = handler
	slog.Debug("registered API handler", "type", descriptor.FullName())
	return nil
}

var deviceInfoHandlers []func(*pb.DeviceInfoResponse) error

// Register a device info handler.  The handlers will be called in unspecified
// order for device info requests.
func RegisterDeviceInfo(handler func(*pb.DeviceInfoResponse) error) {
	deviceInfoHandlers = append(deviceInfoHandlers, handler)
}
