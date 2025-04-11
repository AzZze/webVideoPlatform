package gbs

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	sdp "github.com/panjjo/gosdp"
	"wvp/internal/core/gb28181"
	"wvp/internal/core/sms"
	"wvp/pkg/gbs/m"
	"wvp/pkg/gbs/sip"
	"wvp/pkg/zlm"
)

type PlayInput struct {
	Channel    *gb28181.Channel
	SMS        *sms.MediaServer
	StreamMode int8
}

type StopPlayInput struct {
	Channel *gb28181.Channel
}

func (g *GB28181API) StopPlay(in *StopPlayInput) error {
	ch, ok := g.svr.memoryStorer.GetChannel(in.Channel.DeviceID, in.Channel.ChannelID)
	if !ok {
		return ErrDeviceNotExist
	}

	ch.device.playMutex.Lock()
	defer ch.device.playMutex.Unlock()

	key := "play:" + in.Channel.DeviceID + ":" + in.Channel.ChannelID
	stream, ok := g.streams.LoadAndDelete(key)
	if !ok {
		return nil
	}
	if stream.Resp == nil {
		return nil
	}
	req := sip.NewRequestFromResponse(sip.MethodBYE, stream.Resp)
	req.SetDestination(ch.Source())
	req.SetConnection(ch.Conn())

	tx, err := g.svr.Request(req)
	if err != nil {
		return err
	}
	_, err = sipResponse(tx)
	return err
}

/*
根据设备的设备id和通道id进行播放，
发送请求给zlm服务器进行数据，一般都是设备主动rtmp推流数据
*/
func (g *GB28181API) Play(in *PlayInput) error {
	ch, ok := g.svr.memoryStorer.GetChannel(in.Channel.DeviceID, in.Channel.ChannelID)
	if !ok {
		return ErrDeviceNotExist
	}

	ch.device.playMutex.Lock()
	defer ch.device.playMutex.Unlock()

	// 播放中
	key := "play:" + in.Channel.DeviceID + ":" + in.Channel.ChannelID
	stream, ok := g.streams.LoadOrStore(key, &Streams{})
	if ok {
		return nil
	}

	// 开启RTP服务器等待接收视频流
	resp, err := g.sms.OpenRTPServer(in.SMS, zlm.OpenRTPServerRequest{
		TCPMode:  in.StreamMode,
		StreamID: in.Channel.ID,
	})
	if err != nil {
		return err
	}

	if err := g.sipPlayPush2(ch, in, resp.Port, stream); err != nil {
		return err
	}

	return nil
}

func (g *GB28181API) sipPlayPush2(ch *Channel, in *PlayInput, port int, stream *Streams) error {
	name := "Play"
	protocal := "TCP/RTP/AVP"
	if in.StreamMode == 0 {
		protocal = "RTP/AVP"
	}

	// if  {
	// name = "Playback"
	// protocal = "RTP/RTCP"
	// }

	video := sdp.Media{
		Description: sdp.MediaDescription{
			Type:     "video",
			Port:     port,
			Formats:  []string{"96", "97", "98"},
			Protocol: protocal,
		},
	}
	video.AddAttribute("recvonly")

	switch in.StreamMode {
	case 1:
		video.AddAttribute("setup", "passive")
		video.AddAttribute("connection", "new")
	case 2:
		video.AddAttribute("setup", "active")
		video.AddAttribute("connection", "new")
	}
	video.AddAttribute("rtpmap", "96", "PS/90000")
	video.AddAttribute("rtpmap", "97", "MPEG4/90000")
	video.AddAttribute("rtpmap", "98", "H264/90000")

	// defining message
	msg := &sdp.Message{
		Origin: sdp.Origin{
			Username:    ch.ChannelID, // 媒体服务器id
			NetworkType: "IN",
			AddressType: "IP4",
			Address:     in.SMS.GetSDPIP(),
		},
		Name: name,
		Connection: sdp.ConnectionData{
			NetworkType: "IN",
			AddressType: "IP4",
			IP:          net.ParseIP(in.SMS.GetSDPIP()),
		},
		Timing: []sdp.Timing{
			{
				// 	Start: data.S,
				// End:   data.E,
			},
		},
		Medias: []sdp.Media{video},
		SSRC:   g.getSSRC(0),
		// URI:    fmt.Sprintf("%s:0", channel.ChannelID),
	}

	// appending message to session
	body := msg.Append(nil).AppendTo(nil)
	// appending session to byte buffer
	// uri, _ := sip.ParseURI(channel.URIStr)
	// channel.addr = &sip.Address{URI: uri}
	// _serverDevices.addr.Params.Add("tag", sip.String{Str: sip.RandString(20)})
	tx, err := g.svr.wrapRequest(ch, sip.MethodInvite, &sip.ContentTypeSDP, body, func(r *sip.Request) {
		r.AppendHeader(&sip.GenericHeader{HeaderName: "Subject", Contents: fmt.Sprintf("%s:%s,%s:%s", ch.ChannelID, in.Channel.ID, in.Channel.DeviceID, in.Channel.ID)})
	})
	if err != nil {
		return err
	}
	resp, err := sipResponse(tx)
	if err != nil {
		return err
	}

	if contact, _ := resp.Contact(); contact == nil {
		resp.AppendHeader(&sip.ContactHeader{
			DisplayName: g.svr.fromAddress.DisplayName,
			Address:     &sip.URI{FUser: sip.String{Str: g.cfg.ID}, FHost: g.cfg.Domain},
			Params:      sip.NewParams(),
		})
	}

	stream.Resp = resp

	ackReq := sip.NewRequestFromResponse(sip.MethodACK, resp)
	return tx.Request(ackReq)

	// data.Resp = response
	// // ACK
	// tx.Request(sip.NewRequestFromResponse(sip.MethodACK, response))

	// callid, _ := response.CallID()
	// data.CallID = string(*callid)

	// cseq, _ := response.CSeq()
	// if cseq != nil {
	// 	data.CseqNo = cseq.SeqNo
	// }

	// // from, _ := response.From()
	// // to, _ := response.To()
	// // for k, v := range to.Params.Items() {
	// // 	data.Ttag[k] = v.String()
	// // }
	// // for k, v := range from.Params.Items() {
	// // 	data.Ftag[k] = v.String()
	// // }
	// data.Status = 0

	// return data, err
	// return nil
}

// sip 请求播放
func SipPlay(data *Streams) (*Streams, error) {
	channel := Channels{ChannelID: data.ChannelID}
	// if err := db.Get(db.DBClient, &channel); err != nil {
	// 	if db.RecordNotFound(err) {
	// 		return nil, errors.New("通道不存在")
	// 	}
	// 	return nil, err
	// }

	data.DeviceID = channel.DeviceID
	data.StreamType = channel.StreamType
	// 使用通道的播放模式进行处理
	switch channel.StreamType {
	case m.StreamTypePull:
		// 拉流

	default:
		// 推流模式要求设备在线且活跃
		if time.Now().Unix()-channel.Active > 30*60 || channel.Status != m.DeviceStatusON {
			return nil, errors.New("通道已离线")
		}
		user, ok := _activeDevices.Get(channel.DeviceID)
		if !ok {
			return nil, errors.New("设备已离线")
		}
		// GB28181推流
		if data.StreamID == "" {
			ssrcLock.Lock()
			// data.ssrc =g. getSSRC(data.T)
			data.StreamID = ssrc2stream(data.ssrc)

			// 成功后保存
			// db.Create(db.DBClient, data)
			ssrcLock.Unlock()
		}

		var err error
		data, err = sipPlayPush(data, channel, user)
		if err != nil {
			return nil, fmt.Errorf("获取视频失败:%v", err)
		}
	}

	data.HTTP = fmt.Sprintf("%s/rtp/%s/hls.m3u8", config.Media.HTTP, data.StreamID)
	data.RTMP = fmt.Sprintf("%s/rtp/%s", config.Media.RTMP, data.StreamID)
	data.RTSP = fmt.Sprintf("%s/rtp/%s", config.Media.RTSP, data.StreamID)
	data.WSFLV = fmt.Sprintf("%s/rtp/%s.live.flv", config.Media.WS, data.StreamID)

	data.Ext = time.Now().Unix() + 2*60 // 2分钟等待时间
	StreamList.Response.Store(data.StreamID, data)
	if data.T == 0 {
		StreamList.Succ.Store(data.ChannelID, data)
	}
	// db.Save(db.DBClient, data)
	return data, nil
}

var ssrcLock *sync.Mutex

/*
notice 国标设备在线时 推送流  然后返回zlm的配置流地址、浏览器直接播放zlm的上的地址数据 不经过业务系统
*/
func sipPlayPush(data *Streams, channel Channels, device Devices) (*Streams, error) {
	var (
		s sdp.Session
		b []byte
	)
	name := "Play"
	protocal := "TCP/RTP/AVP"
	if data.T == 1 {
		name = "Playback"
		protocal = "RTP/RTCP"
	}

	video := sdp.Media{
		Description: sdp.MediaDescription{
			Type:     "video",
			Port:     _sysinfo.MediaServerRtpPort,
			Formats:  []string{"96", "98", "97"},
			Protocol: protocal,
		},
	}
	video.AddAttribute("recvonly")
	if data.T == 0 {
		video.AddAttribute("setup", "passive")
		video.AddAttribute("connection", "new")
	}
	video.AddAttribute("rtpmap", "96", "PS/90000")
	video.AddAttribute("rtpmap", "98", "H264/90000")
	video.AddAttribute("rtpmap", "97", "MPEG4/90000")

	// defining message
	msg := &sdp.Message{
		Origin: sdp.Origin{
			Username: _serverDevices.DeviceID, // 媒体服务器id
			Address:  _sysinfo.MediaServerRtpIP.String(),
		},
		Name: name,
		Connection: sdp.ConnectionData{
			IP:  _sysinfo.MediaServerRtpIP,
			TTL: 0,
		},
		Timing: []sdp.Timing{
			{
				Start: data.S,
				End:   data.E,
			},
		},
		Medias: []sdp.Media{video},
		SSRC:   data.ssrc,
	}
	if data.T == 1 {
		msg.URI = fmt.Sprintf("%s:0", channel.ChannelID)
	}

	// appending message to session
	s = msg.Append(s)
	// appending session to byte buffer
	b = s.AppendTo(b)
	uri, _ := sip.ParseURI(channel.URIStr)
	channel.addr = &sip.Address{URI: uri}
	_serverDevices.addr.Params.Add("tag", sip.String{Str: sip.RandString(20)})
	hb := sip.NewHeaderBuilder().SetTo(channel.addr).SetFrom(_serverDevices.addr).AddVia(&sip.ViaHop{
		Params: sip.NewParams().Add("branch", sip.String{Str: sip.GenerateBranch()}),
	}).SetContentType(&sip.ContentTypeSDP).SetMethod(sip.MethodInvite).SetContact(_serverDevices.addr)
	req := sip.NewRequest("", sip.MethodInvite, channel.addr.URI, sip.DefaultSipVersion, hb.Build(), b)
	req.SetDestination(device.source)
	req.AppendHeader(&sip.GenericHeader{HeaderName: "Subject", Contents: fmt.Sprintf("%s:%s,%s:%s", channel.ChannelID, data.StreamID, _serverDevices.DeviceID, data.StreamID)})
	req.SetRecipient(channel.addr.URI)
	tx, err := svr.Request(req)
	if err != nil {
		// logrus.Warningln("sipPlayPush fail.id:", device.DeviceID, channel.ChannelID, "err:", err)
		return data, err
	}
	// response
	response, err := sipResponse(tx)
	if err != nil {
		// logrus.Warningln("sipPlayPush response fail.id:", device.DeviceID, channel.ChannelID, "err:", err)
		return data, err
	}
	data.Resp = response
	// ACK
	tx.Request(sip.NewRequestFromResponse(sip.MethodACK, response))

	callid, _ := response.CallID()
	data.CallID = string(*callid)

	cseq, _ := response.CSeq()
	if cseq != nil {
		data.CseqNo = cseq.SeqNo
	}

	// from, _ := response.From()
	// to, _ := response.To()
	// for k, v := range to.Params.Items() {
	// 	data.Ttag[k] = v.String()
	// }
	// for k, v := range from.Params.Items() {
	// 	data.Ftag[k] = v.String()
	// }
	data.Status = 0

	return data, err
}

// sip 停止播放
func SipStopPlay(ssrc string) {
	zlmCloseStream(ssrc)
	data, ok := StreamList.Response.Load(ssrc)
	if !ok {
		return
	}
	play := data.(*Streams)
	if play.StreamType == m.StreamTypePush {
		// 推流，需要发送关闭请求
		resp := play.Resp
		u, ok := _activeDevices.Load(play.DeviceID)
		if !ok {
			return
		}
		user := u.(Devices)
		req := sip.NewRequestFromResponse(sip.MethodBYE, resp)
		req.SetDestination(user.source)
		tx, err := svr.Request(req)
		if err != nil {
			// logrus.Warningln("sipStopPlay bye fail.id:", play.DeviceID, play.ChannelID, "err:", err)
		}
		_, err = sipResponse(tx)
		if err != nil {
			// logrus.Warnln("sipStopPlay response fail", err)
			play.Msg = err.Error()
		} else {
			play.Status = 1
			play.Stop = true
		}
		// db.Save(db.DBClient, play)
	}
	StreamList.Response.Delete(ssrc)
	if play.T == 0 {
		StreamList.Succ.Delete(play.ChannelID)
	}
}
