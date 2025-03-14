package gb28181

import (
	"context"

	"github.com/ixugo/goweb/pkg/orm"
	"github.com/ixugo/goweb/pkg/web"
	"wvp/internal/core/bz"
	"wvp/internal/core/uniqueid"
)

type GB28181 struct {
	// deviceStore  DeviceStorer
	// channelStore ChannelStorer
	store Storer
	uni   uniqueid.Core
}

func NewGB28181(store Storer, uni uniqueid.Core) GB28181 {
	return GB28181{
		store: store,
		uni:   uni,
	}
}

func (g GB28181) Store() Storer {
	return g.store
}

func (g GB28181) GetDeviceByDeviceID(deviceID string) (*Device, error) {
	ctx := context.TODO()
	var d Device
	if err := g.store.Device().Get(ctx, &d, orm.Where("device_id=?", deviceID)); err != nil {
		if !orm.IsErrRecordNotFound(err) {
			return nil, err
		}
		d.init(g.uni.UniqueID(bz.IDPrefixGB), deviceID)
		if err := g.store.Device().Add(ctx, &d); err != nil {
			return nil, err
		}
	}
	return &d, nil
}

func (g GB28181) Logout(deviceID string, changeFn func(*Device)) error {
	var d Device
	if err := g.store.Device().Edit(context.TODO(), &d, func(d *Device) {
		changeFn(d)
	}, orm.Where("device_id=?", deviceID)); err != nil {
		return err
	}

	return nil
}

func (g GB28181) Edit(deviceID string, changeFn func(*Device)) error {
	var d Device
	if err := g.store.Device().Edit(context.TODO(), &d, func(d *Device) {
		changeFn(d)
	}, orm.Where("device_id=?", deviceID)); err != nil {
		return err
	}

	return nil
}

func (g GB28181) SaveChannels(channels []*Channel) error {
	if len(channels) <= 0 {
		return nil
	}

	var dev Device
	g.store.Device().Edit(context.TODO(), &dev, func(d *Device) {
		d.Channels = len(channels)
	}, orm.Where("device_id=?", channels[0].DeviceID))

	for _, channel := range channels {
		var ch Channel
		if err := g.store.Channel().Edit(context.TODO(), &ch, func(c *Channel) {
			c.IsOnline = channel.IsOnline
			ch.DID = dev.ID
		}, orm.Where("device_id = ? AND channel_id = ?", channel.DeviceID, channel.ChannelID)); err != nil {
			channel.ID = g.uni.UniqueID(bz.IDPrefixGBChannel)
			channel.DID = dev.ID
			g.store.Channel().Add(context.TODO(), channel)
		}
	}
	return nil
}

// FindDevices 获取所有设备
func (g GB28181) FindDevices(ctx context.Context) ([]*Device, error) {
	var devices []*Device
	if _, err := g.store.Device().Find(ctx, &devices, web.NewPagerFilterMaxSize()); err != nil {
		return nil, err
	}
	return devices, nil
}
