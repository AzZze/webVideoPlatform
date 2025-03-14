package sms

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gowvp/gb28181/internal/conf"
	"github.com/gowvp/gb28181/pkg/zlm"
	"github.com/ixugo/goweb/pkg/conc"
	"github.com/ixugo/goweb/pkg/orm"
	"github.com/ixugo/goweb/pkg/web"
)

type WarpMediaServer struct {
	IsOnline      bool
	LastUpdatedAt time.Time
}

type NodeManager struct {
	storer Storer

	zlm          zlm.Engine
	cacheServers conc.Map[string, *WarpMediaServer]
	quit         chan struct{}
}

func NewNodeManager(storer Storer) *NodeManager {
	n := NodeManager{
		storer: storer,
		zlm:    zlm.NewEngine(),
		quit:   make(chan struct{}, 1),
	}
	go n.tickCheck()
	return &n
}

func (n *NodeManager) Close() {
	close(n.quit)
}

// tickCheck 定时检查服务是否离线
func (n *NodeManager) tickCheck() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-n.quit:
			return
		case <-ticker.C:
			// TODO: 前期先固定 10 秒保活，后期优化
			const KeepaliveInterval = 2 * 10 * time.Second
			n.cacheServers.Range(func(serverID string, ms *WarpMediaServer) bool {
				IsOffline := time.Since(ms.LastUpdatedAt) >= KeepaliveInterval
				if ms.IsOnline == IsOffline {
					ms.IsOnline = !IsOffline
					var svr MediaServer
					if err := n.storer.MediaServer().Edit(context.Background(), &svr, func(b *MediaServer) {
						b.Status = ms.IsOnline
					}, orm.Where("id=?", serverID)); err != nil {
						slog.Error("Edit MediaServer err", "err", err)
					}
				}
				return true
			})

		}
	}
}

func (n *NodeManager) Run(cfg *conf.Media, serverPort int) error {
	ctx := context.Background()

	setValueFn := func(ms *MediaServer) {
		ms.ID = DefaultMediaServerID
		ms.IP = cfg.IP
		ms.Ports.HTTP = cfg.HTTPPort
		ms.Secret = cfg.Secret
		ms.Type = "zlm"
		ms.Status = false
		ms.RTPPortRange = cfg.RTPPortRange
		ms.HookIP = cfg.WebHookIP
		ms.SDPIP = cfg.SDPIP
	}

	var ms MediaServer
	if err := n.storer.MediaServer().Edit(ctx, &ms, func(b *MediaServer) {
		setValueFn(b)
	}, orm.Where("id=?", DefaultMediaServerID)); err != nil {
		if !orm.IsErrRecordNotFound(err) {
			return err
		}
		setValueFn(&ms)
		if err := n.storer.MediaServer().Add(ctx, &ms); err != nil {
			return err
		}
	}

	mediaServers, _, err := n.findMediaServer(ctx, &FindMediaServerInput{
		PagerFilter: web.NewPagerFilterMaxSize(),
	})
	if err != nil {
		return err
	}

	for _, ms := range mediaServers {
		go n.connection(ms, serverPort)
	}

	return nil
}

func (n *NodeManager) connection(server *MediaServer, serverPort int) {
	n.cacheServers.Store(server.ID, &WarpMediaServer{
		LastUpdatedAt: time.Now(),
	})

	url := fmt.Sprintf("http://%s:%d", server.IP, server.Ports.HTTP)
	engine := n.zlm.SetConfig(zlm.Config{
		URL:    url,
		Secret: "s1kPE7bzqKeHUaVcp8dCA0jeB8yxyFq4",
	})

	log := slog.With("url", url, "id", server.ID)

	log.Info("ZLM 服务节点连接中")

	for {
		resp, err := engine.GetServerConfig()
		if err != nil {
			log.Error("ZLM 服务节点连接失败", "err", err)
			time.Sleep(10 * time.Second)
			continue
		}
		log.Info("ZLM 服务节点连接成功")

		zlmConfig := resp.Data[0]
		var ms MediaServer
		if err := n.storer.MediaServer().Edit(context.Background(), &ms, func(b *MediaServer) {
			// b.Ports.FLV = zlmConfig.HTTPPort
			// TODO: 映射的端口，会导致获取配置文件的端口不一定能访问
			http := server.Ports.HTTP
			b.Ports.FLV = http
			b.Ports.WsFLV = http //   zlmConfig.HTTPSslport
			b.Ports.HTTPS = zlmConfig.HTTPSslport
			b.Ports.RTMP = zlmConfig.RtmpPort
			b.Ports.RTMPs = zlmConfig.RtmpSslport
			b.Ports.RTSP = zlmConfig.RtspPort
			b.Ports.RTSPs = zlmConfig.RtspSslport
			b.Ports.RTPPorxy = zlmConfig.RtpProxyPort
			b.Ports.FLVs = zlmConfig.HTTPSslport
			b.Ports.WsFLVs = zlmConfig.HTTPSslport
			b.HookAliveInterval = 10
			b.Status = true
		}, orm.Where("id=?", server.ID)); err != nil {
			panic(fmt.Errorf("保存 MediaServer 失败 %w", err))
		}

		log.Info("ZLM 服务节点配置设置")

		hookPrefix := fmt.Sprintf("http://%s:%d/webhook", server.HookIP, serverPort)
		req := zlm.SetServerConfigRequest{
			RtcExternIP:          zlm.NewString(server.IP),
			GeneralMediaServerID: zlm.NewString(server.ID),
			HookEnable:           zlm.NewString("1"),
			HookOnFlowReport:     zlm.NewString(""),
			HookOnPlay:           zlm.NewString(fmt.Sprintf("%s/on_play", hookPrefix)),
			// HookOnHTTPAccess:     zlm.NewString(""),
			HookOnPublish:          zlm.NewString(fmt.Sprintf("%s/on_publish", hookPrefix)),
			HookOnStreamNoneReader: zlm.NewString(fmt.Sprintf("%s/on_stream_none_reader", hookPrefix)),
			HookOnRecordTs:         zlm.NewString(""),
			HookOnRtspAuth:         zlm.NewString(""),
			HookOnRtspRealm:        zlm.NewString(""),
			// HookOnServerStarted: ,
			HookOnShellLogin:    zlm.NewString(""),
			HookOnStreamChanged: zlm.NewString(fmt.Sprintf("%s/on_stream_changed", hookPrefix)),
			// HookOnStreamNotFound: ,
			HookOnServerKeepalive: zlm.NewString(fmt.Sprintf("%s/on_server_keepalive", hookPrefix)),
			// HookOnSendRtpStopped: ,
			// HookOnRtpServerTimeout: ,
			// HookOnRecordMp4: ,
			HookTimeoutSec: zlm.NewString("20"),
			// TODO: 回调时间间隔有问题
			HookAliveInterval: zlm.NewString(fmt.Sprint(server.HookAliveInterval)),
			// 推流断开后可以在超时时间内重新连接上继续推流，这样播放器会接着播放。
			// 置0关闭此特性(推流断开会导致立即断开播放器)
			// 此参数不应大于播放器超时时间
			// 优化此消息以更快的收到流注销事件
			ProtocolContinuePushMs: zlm.NewString("3000"),
			RtpProxyPortRange:      &server.RTPPortRange,
		}

		{
			resp, err := engine.SetServerConfig(&req)
			if err != nil {
				log.Error("ZLM 服务节点配置设置失败", "err", err)
				time.Sleep(10 * time.Second)
				continue
			}

			log.Info("ZLM 服务节点配置设置成功", "changed", resp.Changed)
		}

		return
	}
}

func (n *NodeManager) Keepalive(serverID string) {
	value, ok := n.cacheServers.Load(serverID)
	if !ok {
		return
	}
	value.LastUpdatedAt = time.Now()
}

// findMediaServer Paginated search
func (n *NodeManager) findMediaServer(ctx context.Context, in *FindMediaServerInput) ([]*MediaServer, int64, error) {
	items := make([]*MediaServer, 0)
	total, err := n.storer.MediaServer().Find(ctx, &items, in)
	if err != nil {
		return nil, 0, web.ErrDB.Withf(`Find err[%s]`, err.Error())
	}
	return items, total, nil
}

// OpenRTPServer 开启RTP服务器
func (n *NodeManager) OpenRTPServer(server *MediaServer, in zlm.OpenRTPServerRequest) (*zlm.OpenRTPServerResponse, error) {
	addr := fmt.Sprintf("http://%s:%d", server.IP, server.Ports.HTTP)
	e := n.zlm.SetConfig(zlm.Config{
		URL:    addr,
		Secret: server.Secret,
	})
	return e.OpenRTPServer(in)
}

// CloseRTPServer 关闭RTP服务器
func (n *NodeManager) CloseRTPServer(in zlm.CloseRTPServerRequest) (*zlm.CloseRTPServerResponse, error) {
	return n.zlm.CloseRTPServer(in)
}

// AddStreamProxy 添加流代理
func (n *NodeManager) AddStreamProxy(server *MediaServer, in zlm.AddStreamProxyRequest) (*zlm.AddStreamProxyResponse, error) {
	addr := fmt.Sprintf("http://%s:%d", server.IP, server.Ports.HTTP)
	e := n.zlm.SetConfig(zlm.Config{
		URL:    addr,
		Secret: server.Secret,
	})
	return e.AddStreamProxy(in)
}
