package gbs

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ixugo/goweb/pkg/conc"
	"github.com/ixugo/goweb/pkg/orm"
	"wvp/internal/conf"
	"wvp/internal/core/gb28181"
	"wvp/internal/core/sms"
	"wvp/pkg/gbs/sip"
)

const ignorePassword = "#"

type GB28181API struct {
	cfg  *conf.SIP
	core gb28181.GB28181

	catalog *sip.Collector[Channels]

	// TODO: 待替换成 redis
	streams *conc.Map[string, *Streams]

	svr *Server

	sms *sms.NodeManager
}

func NewGB28181API(cfg *conf.Bootstrap, store gb28181.GB28181, sms *sms.NodeManager) *GB28181API {
	g := GB28181API{
		cfg:  &cfg.Sip,
		core: store,
		sms:  sms,
		catalog: sip.NewCollector[Channels](func(c1, c2 *Channels) bool {
			return c1.ChannelID == c2.ChannelID
		}),
		streams: &conc.Map[string, *Streams]{},
	}
	go g.catalog.Start(func(s string, c []*Channels) {
		// 零值不做变更，没有通道又何必注册上来
		if len(c) == 0 {
			return
		}

		// ipc, ok := g.svr.devices.Load(s)
		// if ok {
		// 	ipc.channels.Clear()
		// 	for _, ch := range c {
		// 		ch := Channel{
		// 			ChannelID: ch.ChannelID,
		// 			device:    ipc,
		// 		}
		// 		ch.init(g.cfg.Domain)
		// 		ipc.channels.Store(ch.ChannelID, &ch)
		// 	}
		// }

		out := make([]*gb28181.Channel, len(c))
		for i, ch := range c {
			out[i] = &gb28181.Channel{
				DeviceID:  s,
				ChannelID: ch.ChannelID,
				Name:      ch.Name,
				IsOnline:  ch.Status == "OK" || ch.Status == "ON",
				Ext: gb28181.DeviceExt{
					Manufacturer: ch.Manufacturer,
					Model:        ch.Model,
				},
			}
		}
		g.core.SaveChannels(out)
	})
	return &g
}

func (g *GB28181API) handlerRegister(ctx *sip.Context) {
	if len(ctx.DeviceID) < 18 {
		ctx.String(http.StatusBadRequest, "device id too short")
		return
	}

	dev, err := g.core.GetDeviceByDeviceID(ctx.DeviceID)
	if err != nil {
		ctx.Log.Error("GetDeviceByDeviceID", "err", err)
		ctx.String(http.StatusInternalServerError, "server db error")
		return
	}

	password := dev.Password
	if password == "" {
		password = g.cfg.Password
	}
	// 免鉴权
	if dev.Password == ignorePassword {
		password = ""
	}
	if password != "" {
		hdrs := ctx.Request.GetHeaders("Authorization")
		if len(hdrs) == 0 {
			resp := sip.NewResponseFromRequest("", ctx.Request, http.StatusUnauthorized, http.StatusText(http.StatusUnauthorized), nil)
			resp.AppendHeader(&sip.GenericHeader{HeaderName: "WWW-Authenticate", Contents: fmt.Sprintf("Digest nonce=\"%s\", algorithm=MD5, realm=\"%s\",qop=\"auth\"", sip.RandString(32), g.cfg.Domain)})
			_ = ctx.Tx.Respond(resp)
			return
		}
		authenticateHeader := hdrs[0].(*sip.GenericHeader)
		auth := sip.AuthFromValue(authenticateHeader.Contents)
		auth.SetPassword(password)
		auth.SetUsername(dev.DeviceID)
		auth.SetMethod(ctx.Request.Method())
		auth.SetURI(auth.Get("uri"))
		if auth.CalcResponse() != auth.Get("response") {
			ctx.Log.Info("设备注册鉴权失败")
			ctx.String(http.StatusUnauthorized, "wrong password")
			return
		}
	}

	respFn := func() {
		resp := sip.NewResponseFromRequest("", ctx.Request, http.StatusOK, "OK", nil)
		resp.AppendHeader(&sip.GenericHeader{
			HeaderName: "Date",
			Contents:   time.Now().Format("2006-01-02T15:04:05.000"),
		})
		_ = ctx.Tx.Respond(resp)
	}

	expire := ctx.GetHeader("Expires")
	if expire == "0" {
		ctx.Log.Info("设备注销")
		g.logout(ctx.DeviceID, func(b *gb28181.Device) {
			b.IsOnline = false
			b.Address = ctx.Source.String()
		})
		respFn()
		return
	}
	g.login(ctx, expire)

	conn := ctx.Request.GetConnection()
	fmt.Printf(">>> %p\n", conn)

	ctx.Log.Info("设备注册成功")

	respFn()

	g.QueryDeviceInfo(ctx)
	g.QueryCatalog(dev.DeviceID)
}

func (g GB28181API) login(ctx *sip.Context, expire string) {
	slog.Info("status change 设备上线", "device_id", ctx.DeviceID)
	g.svr.memoryStorer.Change(ctx.DeviceID, func(d *gb28181.Device) {
		d.IsOnline = true
		d.RegisteredAt = orm.Now()
		d.KeepaliveAt = orm.Now()
		d.Expires, _ = strconv.Atoi(expire)
	}, func(d *Device) {
		d.conn = ctx.Request.GetConnection()
		d.source = ctx.Source
		d.to = ctx.To
	})
}

func (g GB28181API) logout(deviceID string, changeFn func(*gb28181.Device)) error {
	slog.Info("status change 设备离线", "device_id", deviceID)
	return g.svr.memoryStorer.Change(deviceID, changeFn, func(d *Device) {
		d.conn = nil
		d.source = nil
		d.to = nil
		d.Expires = 0
	})
}
