package gb28181cache

import (
	"context"

	"github.com/ixugo/goweb/pkg/orm"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"wvp/internal/core/gb28181"
	"wvp/pkg/gbs"
)

var _ gb28181.DeviceStorer = &Device{}

type Device Cache

// Add implements gb28181.DeviceStorer.
func (d *Device) Add(ctx context.Context, dev *gb28181.Device) error {
	if err := d.Storer.Device().Add(ctx, dev); err != nil {
		return err
	}
	d.devices.LoadOrStore(dev.DeviceID, gbs.NewDevice(nil, dev))
	return nil
}

// Del implements gb28181.DeviceStorer.
func (d *Device) Del(ctx context.Context, dev *gb28181.Device, opts ...orm.QueryOption) error {
	if err := d.Storer.Device().Session(
		ctx,
		func(tx *gorm.DB) error {
			db := tx.Clauses(clause.Returning{})
			for _, fn := range opts {
				db = fn(db)
			}
			return db.Delete(dev).Error
		},
		func(tx *gorm.DB) error {
			return tx.Model(&gb28181.Channel{}).Where("did=?", dev.ID).Delete(nil).Error
		},
	); err != nil {
		return err
	}

	d.devices.Delete(dev.DeviceID)
	return nil
}

// Edit implements gb28181.DeviceStorer.
func (d *Device) Edit(ctx context.Context, dev *gb28181.Device, changeFn func(*gb28181.Device), opts ...orm.QueryOption) error {
	return d.Storer.Device().Edit(ctx, dev, changeFn, opts...)
}

// Find implements gb28181.DeviceStorer.
func (d *Device) Find(ctx context.Context, devs *[]*gb28181.Device, pager orm.Pager, opts ...orm.QueryOption) (int64, error) {
	return d.Storer.Device().Find(ctx, devs, pager, opts...)
}

// Get implements gb28181.DeviceStorer.
func (d *Device) Get(ctx context.Context, dev *gb28181.Device, opts ...orm.QueryOption) error {
	return d.Storer.Device().Get(ctx, dev, opts...)
}

// Session implements gb28181.DeviceStorer.
func (d *Device) Session(ctx context.Context, changeFns ...func(*gorm.DB) error) error {
	return d.Storer.Device().Session(ctx, changeFns...)
}
