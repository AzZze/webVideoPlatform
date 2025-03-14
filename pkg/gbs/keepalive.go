package gbs

import (
	"github.com/ixugo/goweb/pkg/orm"
	"wvp/internal/core/gb28181"
	"wvp/pkg/gbs/sip"
	// "github.com/panjjo/gosip/db"
)

// MessageNotify 心跳包xml结构
type MessageNotify struct {
	CmdType  string `xml:"CmdType"`
	SN       int    `xml:"SN"`
	DeviceID string `xml:"DeviceID"`
	Status   string `xml:"Status"`
	Info     string `xml:"Info"`
}

func (g *GB28181API) sipMessageKeepalive(ctx *sip.Context) {
	var msg MessageNotify
	if err := sip.XMLDecode(ctx.Request.Body(), &msg); err != nil {
		ctx.Log.Error("Message Unmarshal xml err", "err", err)
		return
	}

	if err := g.svr.memoryStorer.Change(ctx.DeviceID, func(d *gb28181.Device) {
		d.KeepaliveAt = orm.Now()
		d.IsOnline = msg.Status == "OK" || msg.Status == "ON"
		d.Address = ctx.Source.String()
		d.Trasnport = ctx.Source.Network()
	}, func(d *Device) {
	}); err != nil {
		ctx.Log.Error("keepalive", "err", err)
	}

	ctx.String(200, "OK")
}
