// The `bluetooth_proxy` component implements the ESPHome bluetooth proxy
// protocol for use with Home Assistant.  Enabling this component will also
// automatically enable the `api` component.
// At this time, only passive scans are implemented.
package bluetooth_proxy

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/mook/mockesphome/api"
	"github.com/mook/mockesphome/api/pb"
	"github.com/mook/mockesphome/components"
	"google.golang.org/protobuf/proto"
	"tinygo.org/x/bluetooth"
)

// Configuration for the component.
type Configuration struct{}

// Bluetooth proxy component.
type component struct {
	config  Configuration
	adapter *bluetooth.Adapter
}

type proxyFeatureFlag uint32

const (
	proxyFeaturePassiveScan = proxyFeatureFlag(1 << iota)
	proxyFeatureActiveConnections
	proxyFeatureRemoteCaching
	proxyFeaturePairing
	proxyFeatureCacheClearing
	proxyFeatureRawAdvertisements
	proxyFeatureStateAndMode
)

type proxySubscriptionFlag uint32

const (
	proxySubscriptionRawAdvertisements = proxySubscriptionFlag(1 << iota)
)

func (c *component) ID() string {
	return "bluetooth_proxy"
}

func (c *component) Dependencies() []string {
	return []string{"api"}
}

func (c *component) Configure(ctx context.Context, load func(any) error) error {
	return load(&c.config)
}

func (c *component) Start(ctx context.Context) error {
	if c.adapter == nil {
		c.adapter = bluetooth.DefaultAdapter
	}
	if err := c.adapter.Enable(); err != nil {
		return err
	}
	handlers := []struct {
		proto.Message
		api.MessageHandler
	}{
		{&pb.SubscribeBluetoothLEAdvertisementsRequest{}, c.handleSubscribeBluetoothLEAdvertisements},
		{&pb.UnsubscribeBluetoothLEAdvertisementsRequest{}, c.handleUnsubscribeBluetoothLEAdvertisements},
	}
	for _, handler := range handlers {
		if err := api.RegisterHandler(handler.Message, handler.MessageHandler); err != nil {
			return err
		}
	}
	api.RegisterDeviceInfo(func(dir *pb.DeviceInfoResponse) error {
		dir.SetBluetoothProxyFeatureFlags(
			uint32(proxyFeaturePassiveScan),
		)
		if addr, err := c.adapter.Address(); err == nil {
			dir.SetBluetoothMacAddress(addr.String())
		} else {
			return err
		}
		return nil
	})

	return nil
}

func (c *component) handleSubscribeBluetoothLEAdvertisements(ctx context.Context, msg proto.Message, send api.MessageSender) error {
	if _, ok := msg.(*pb.SubscribeBluetoothLEAdvertisementsRequest); !ok {
		return fmt.Errorf("message is not a SubscribeBluetoothLEAdvertisementsRequest")
	}
	go func() {
		slog.DebugContext(ctx, "scanning bluetooth...")
		if err := c.adapter.Scan(c.createScanResultCallback(ctx, send)); err != nil {
			slog.ErrorContext(ctx, "error scanning", "error", err)
		}
	}()
	return nil
}

func bleAddressToUint64(addr bluetooth.MAC) uint64 {
	return 0 |
		uint64(addr[5])<<40 |
		uint64(addr[4])<<32 |
		uint64(addr[3])<<24 |
		uint64(addr[2])<<16 |
		uint64(addr[1])<<8 |
		uint64(addr[0])
}

func (c *component) createScanResultCallback(ctx context.Context, send api.MessageSender) func(*bluetooth.Adapter, bluetooth.ScanResult) {
	return func(a *bluetooth.Adapter, result bluetooth.ScanResult) {
		resp := &pb.BluetoothLEAdvertisementResponse{}
		resp.SetAddress(bleAddressToUint64(result.Address.MAC))
		resp.SetName(result.LocalName())
		resp.SetRssi(int32(result.RSSI))
		var serviceData []*pb.BluetoothServiceData
		for _, sd := range result.ServiceData() {
			data := &pb.BluetoothServiceData{}
			data.SetUuid(sd.UUID.String())
			data.SetData(sd.Data)
			serviceData = append(serviceData, data)
		}
		resp.SetServiceData(serviceData)
		var manufacturerData []*pb.BluetoothServiceData
		for _, md := range result.ManufacturerData() {
			data := &pb.BluetoothServiceData{}
			data.SetUuid(fmt.Sprintf("0x%04X", md.CompanyID))
			data.SetData(md.Data)
			manufacturerData = append(manufacturerData, data)
		}
		resp.SetManufacturerData(manufacturerData)

		payloadValue := reflect.ValueOf(result.AdvertisementPayload).Elem()
		fieldsValue := payloadValue.FieldByName("AdvertisementFields")
		fields, ok := fieldsValue.Interface().(bluetooth.AdvertisementFields)
		if ok {
			var serviceUuids []string
			for _, uuid := range fields.ServiceUUIDs {
				serviceUuids = append(serviceUuids, uuid.String())
			}
			resp.SetServiceUuids(serviceUuids)
		}

		if err := send(resp); err != nil {
			slog.ErrorContext(ctx, "failed to send bluetooth scan result", "error", err)
		}
	}
}

func (c *component) handleUnsubscribeBluetoothLEAdvertisements(ctx context.Context, msg proto.Message, send api.MessageSender) error {
	if _, ok := msg.(*pb.UnsubscribeBluetoothLEAdvertisementsRequest); !ok {
		return fmt.Errorf("message is not a UnsubscribeBluetoothLEAdvertisementsRequest")
	}
	if err := c.adapter.StopScan(); err != nil {
		return err
	}
	return nil
}

func init() {
	components.Register(&component{})
}
