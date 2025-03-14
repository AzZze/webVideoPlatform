package gb28181cache

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ixugo/goweb/pkg/conc"
	"github.com/ixugo/goweb/pkg/orm"
	"github.com/ixugo/goweb/pkg/web"
	"wvp/internal/core/gb28181"
	"wvp/pkg/gbs"
	"wvp/pkg/gbs/sip"
)

var (
	_ gbs.MemoryStorer = &Cache{}
	_ gb28181.Storer   = &Cache{}
)

type Cache struct {
	gb28181.Storer

	devices *conc.Map[string, *gbs.Device]
}

func (c *Cache) Device() gb28181.DeviceStorer {
	return (*Device)(c)
}

func (c *Cache) Channel() gb28181.ChannelStorer {
	return (*Channel)(c)
}

func NewCache(store gb28181.Storer) *Cache {
	return &Cache{
		Storer:  store,
		devices: &conc.Map[string, *gbs.Device]{},
	}
}

// LoadDeviceToMemory implements gbs.MemoryStorer.
func (c *Cache) LoadDeviceToMemory(conn sip.Connection) {
	devices := make([]*gb28181.Device, 0, 100)
	_, err := c.Storer.Device().Find(context.TODO(), &devices, web.NewPagerFilterMaxSize())
	if err != nil {
		panic(err)
	}

	for _, d := range devices {
		if strings.ToLower(d.Trasnport) == "tcp" {
			// 通知相关设备/通道离线
			continue
		}

		dev := gbs.NewDevice(conn, d)
		if dev != nil {
			slog.Debug("load device to memory", "device_id", d.DeviceID, "to", dev.To())
			channels := make([]*gb28181.Channel, 0, 8)
			_, err := c.Storer.Channel().Find(context.TODO(), &channels, web.NewPagerFilterMaxSize(), orm.Where("device_id=?", d.DeviceID))
			if err != nil {
				panic(err)
			}
			dev.LoadChannels(channels...)
			c.devices.Store(d.DeviceID, dev)
		}
	}
}

// RangeDevices implements gbs.MemoryStorer.
func (c *Cache) RangeDevices(fn func(key string, value *gbs.Device) bool) {
	c.devices.Range(fn)
}

// Change implements gbs.MemoryStorer.
func (c *Cache) Change(deviceID string, changeFn func(*gb28181.Device), changeFn2 func(*gbs.Device)) error {
	var dev gb28181.Device
	if err := c.Storer.Device().Edit(context.TODO(), &dev, changeFn, orm.Where("device_id=?", deviceID)); err != nil {
		return err
	}

	dev2, ok := c.devices.Load(deviceID)
	if !ok {
		return fmt.Errorf("device not found")
	}
	dev2.IsOnline = dev.IsOnline
	dev2.LastKeepaliveAt = dev.KeepaliveAt.Time
	dev2.LastRegisterAt = dev.RegisteredAt.Time
	dev2.Expires = dev.Expires
	changeFn2(dev2)

	if !dev2.IsOnline {
		if err := c.Storer.Channel().BatchEdit(context.TODO(), "is_online", false, orm.Where("did=?", dev.ID)); err != nil {
			slog.Error("更新通道离线状态失败", "error", err)
		}
	}
	return nil
}

// GetChannel implements gbs.MemoryStorer.
func (c *Cache) GetChannel(deviceID string, channelID string) (*gbs.Channel, bool) {
	dev, ok := c.devices.Load(deviceID)
	if !ok {
		return nil, false
	}
	return dev.GetChannel(channelID)
}

// Load implements gbs.MemoryStorer.
func (c *Cache) Load(deviceID string) (*gbs.Device, bool) {
	return c.devices.Load(deviceID)
}

// Store implements gbs.MemoryStorer.
func (c *Cache) Store(deviceID string, value *gbs.Device) {
	c.devices.Store(deviceID, value)
}
