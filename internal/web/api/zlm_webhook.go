package api

import (
	"log/slog"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/ixugo/goweb/pkg/web"
	"wvp/internal/conf"
	"wvp/internal/core/gb28181"
	"wvp/internal/core/media"
	"wvp/internal/core/sms"
	"wvp/pkg/gbs"
)

type WebHookAPI struct {
	smsCore     sms.Core
	mediaCore   media.Core
	gb28181Core gb28181.Core
	conf        *conf.Bootstrap
	log         *slog.Logger
	gbs         *gbs.Server
}

func NewWebHookAPI(core sms.Core, mediaCore media.Core, conf *conf.Bootstrap, gbs *gbs.Server, gb28181 gb28181.Core) WebHookAPI {
	return WebHookAPI{
		smsCore:     core,
		mediaCore:   mediaCore,
		conf:        conf,
		log:         slog.With("hook", "zlm"),
		gbs:         gbs,
		gb28181Core: gb28181,
	}
}

func registerZLMWebhookAPI(r gin.IRouter, api WebHookAPI, handler ...gin.HandlerFunc) {
	{
		group := r.Group("/webhook", handler...)
		group.POST("/on_server_keepalive", web.WarpH(api.onServerKeepalive))
		group.POST("/on_stream_changed", web.WarpH(api.onStreamChanged))
		group.POST("/on_publish", web.WarpH(api.onPublish))
		group.POST("/on_play", web.WarpH(api.onPlay))
		group.POST("/on_stream_none_reader", web.WarpH(api.onStreamNoneReader))
		group.POST("/on_rtp_server_timeout", web.WarpH(api.onRTPServerTimeout))
	}
}

// onServerKeepalive 服务器定时上报时间，上报间隔可配置，默认 10s 上报一次
// https://docs.zlmediakit.com/zh/guide/media_server/web_hook_api.html#_16%E3%80%81on-server-keepalive
func (w WebHookAPI) onServerKeepalive(_ *gin.Context, in *onServerKeepaliveInput) (DefaultOutput, error) {
	w.smsCore.Keepalive(in.MediaServerID)
	return newDefaultOutputOK(), nil
}

// onPublish rtsp/rtmp/rtp 推流鉴权事件。
// https://docs.zlmediakit.com/zh/guide/media_server/web_hook_api.html#_7%E3%80%81on-publish
func (w WebHookAPI) onPublish(c *gin.Context, in *onPublishInput) (*onPublishOutput, error) {
	w.log.Info("推流鉴权", "app", in.App, "stream", in.Stream, "schema", in.Schema, "mediaServerID", in.MediaServerID)
	if in.Schema == "rtmp" {
		params, err := url.ParseQuery(in.Params)
		if err != nil {
			return &onPublishOutput{DefaultOutput: DefaultOutput{Code: 1, Msg: err.Error()}}, nil
		}
		sign := params.Get("sign")
		if err := w.mediaCore.Publish(c.Request.Context(), media.PublishInput{
			App:           in.App,
			Stream:        in.Stream,
			MediaServerID: in.MediaServerID,
			Sign:          sign,
			Secret:        w.conf.Server.RTMPSecret,
			Session:       params.Get("session"),
		}); err != nil {
			return &onPublishOutput{DefaultOutput: DefaultOutput{Code: 1, Msg: err.Error()}}, nil
		}
	}
	return &onPublishOutput{
		DefaultOutput: newDefaultOutputOK(),
	}, nil
}

// onStreamChanged rtsp/rtmp 流注册或注销时触发此事件；此事件对回复不敏感。
// https://docs.zlmediakit.com/zh/guide/media_server/web_hook_api.html#_12%E3%80%81on-stream-changed
func (w WebHookAPI) onStreamChanged(c *gin.Context, in *onStreamChangedInput) (DefaultOutput, error) {
	w.log.Info("流状态变化", "app", in.App, "stream", in.Stream, "schema", in.Schema, "mediaServerID", in.MediaServerID)
	if in.App == "rtp" {
		if !in.Regist {
			ch, err := w.gb28181Core.GetChannel(c.Request.Context(), in.Stream)
			if err != nil {
				w.log.Warn("获取通道失败", "err", err)
				return newDefaultOutputOK(), nil
			}
			w.gbs.StopPlay(&gbs.StopPlayInput{Channel: ch})
		}
		return newDefaultOutputOK(), nil
	}

	switch in.Schema {
	case "rtmp":
		if !in.Regist {
			if err := w.mediaCore.UnPublish(c.Request.Context(), in.App, in.Stream); err != nil {
				slog.Error("UnPublish", "err", err)
			}
		}
	case "rtsp":
	}
	return newDefaultOutputOK(), nil
}

// onPlay rtsp/rtmp/http-flv/ws-flv/hls 播放触发播放器身份验证事件。
// 播放流时会触发此事件。如果流不存在，则首先触发 on_play 事件，然后触发 on_stream_not_found 事件。
// 播放rtsp流时，如果该流开启了rtsp专用认证（on_rtsp_realm），则不会触发on_play事件。
// https://docs.zlmediakit.com/guide/media_server/web_hook_api.html#_6-on-play
func (w WebHookAPI) onPlay(c *gin.Context, in *onPublishInput) (DefaultOutput, error) {
	return newDefaultOutputOK(), nil
	if in.App == "rtp" {
		return newDefaultOutputOK(), nil
	}

	switch in.Schema {
	case "rtmp":
		params, err := url.ParseQuery(in.Params)
		if err != nil {
			slog.Info("onPlay 鉴权失败", "err", err)
			return DefaultOutput{Code: 1, Msg: err.Error()}, nil
		}
		session := params.Get("session")
		if err := w.mediaCore.OnPlay(c.Request.Context(), media.OnPlayInput{
			App:     in.App,
			Stream:  in.Stream,
			Session: session,
		}); err != nil {
			slog.Info("onPlay 鉴权失败", "err", err)
			return DefaultOutput{Code: 1, Msg: err.Error()}, nil
		}
	case "rtsp":
	}

	return newDefaultOutputOK(), nil
}

// onStreamNoneReader 流无人观看时事件，用户可以通过此事件选择是否关闭无人看的流。
// 一个直播流注册上线了，如果一直没人观看也会触发一次无人观看事件，触发时的协议 schema 是随机的，
// 看哪种协议最晚注册(一般为 hls)。
// 后续从有人观看转为无人观看，触发协议 schema 为最后一名观看者使用何种协议。
// 目前 mp4/hls 录制不当做观看人数(mp4 录制可以通过配置文件 mp4_as_player 控制，
// 但是 rtsp/rtmp/rtp 转推算观看人数，也会触发该事件。
// https://docs.zlmediakit.com/zh/guide/media_server/web_hook_api.html#_12%E3%80%81on-stream-changed
func (w WebHookAPI) onStreamNoneReader(c *gin.Context, in *onStreamNoneReaderInput) (onStreamNoneReaderOutput, error) {
	// rtmp 无人观看时，也允许推流
	w.log.Info("无人观看", "app", in.App, "stream", in.Stream, "mediaServerID", in.MediaServerID)
	// 存在录像计划时，不关闭流
	return onStreamNoneReaderOutput{Close: true}, nil
}

// onRTPServerTimeout RTP 服务器超时事件
// 调用 openRtpServer 接口，rtp server 长时间未收到数据,执行此 web hook,对回复不敏感
// https://docs.zlmediakit.com/zh/guide/media_server/web_hook_api.html#_17%E3%80%81on-rtp-server-timeout
func (w WebHookAPI) onRTPServerTimeout(c *gin.Context, in *onRTPServerTimeoutInput) (DefaultOutput, error) {
	w.log.Info("rtp 收流超时", "local_port", in.LocalPort, "ssrc", in.SSRC, "stream_id", in.StreamID, "mediaServerID", in.MediaServerID)
	return newDefaultOutputOK(), nil
}
