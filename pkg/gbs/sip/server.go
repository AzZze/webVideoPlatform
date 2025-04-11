package sip

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/ixugo/goweb/pkg/conc"
)

var bufferSize uint16 = 65535 - 20 - 8 // IPv4 max size - IPv4 Header size - UDP Header size
/*
1. 服务器初始化 ( server.go )
- 监听 UDP/TCP 端口
- 处理 SIP 消息
- 管理设备连接
*/
// Server sip 服务器结构体，包含了服务器运行所需的核心组件
type Server struct {
	udpaddr net.Addr   // UDP服务器地址
	udpConn Connection // UDP连接实例

	txs *transacionts // 事务管理器

	route conc.Map[string, []HandlerFunc] // 路由表，存储不同方法对应的处理函数

	port *Port  // UDP端口
	host net.IP // 服务器IP地址

	tcpPort     *Port            // TCP端口
	tcpListener *net.TCPListener // TCP监听器

	tcpaddr net.Addr // TCP服务器地址

	ctx    context.Context    // 上下文，用于控制服务器生命周期
	cancel context.CancelFunc // 取消函数

	from *Address // 服务器地址信息
}

// NewServer sip server
func NewServer(form *Address) *Server {
	activeTX = &transacionts{txs: map[string]*Transaction{}, rwm: &sync.RWMutex{}}
	ctx, cancel := context.WithCancel(context.TODO())
	srv := &Server{
		txs:    activeTX,
		ctx:    ctx,
		cancel: cancel,
		from:   form,
	}
	return srv
}

func (s *Server) addRoute(method string, handler ...HandlerFunc) {
	s.route.Store(strings.ToUpper(method), handler)
}

func (s *Server) Register(handler ...HandlerFunc) {
	s.addRoute(MethodRegister, handler...)
}

func (s *Server) Message(handler ...HandlerFunc) *RouteGroup {
	s.addRoute(MethodMessage, handler...)
	return newRouteGroup(MethodMessage, s, handler...)
}

func (s *Server) Notify(handler ...HandlerFunc) *RouteGroup {
	s.addRoute(MethodNotify, handler...)
	return newRouteGroup(MethodNotify, s, handler...)
}

func (s *Server) getTX(key string) *Transaction {
	return s.txs.getTX(key)
}

func (s *Server) mustTX(msg *Request) *Transaction {
	key := getTXKey(msg)
	tx := s.txs.getTX(key)

	if tx == nil {
		if msg.conn.Network() == "udp" {
			tx = s.txs.newTX(key, s.udpConn)
		} else {
			tx = s.txs.newTX(key, msg.conn)
		}
	}
	return tx
}

func (s *Server) UDPConn() Connection {
	return s.udpConn
}

// ListenUDPServer ListenUDPServer
func (s *Server) ListenUDPServer(addr string) {
	// 解析UDP地址
	udpaddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		panic(fmt.Errorf("net.ResolveUDPAddr err[%w]", err))
	}
	// 设置端口和主机地址
	s.port = NewPort(udpaddr.Port)
	s.host, err = ResolveSelfIP()
	if err != nil {
		panic(fmt.Errorf("net.ListenUDP resolveip err[%w]", err))
	}
	// 创建UDP监听器
	udp, err := net.ListenUDP("udp", udpaddr)
	if err != nil {
		panic(fmt.Errorf("net.ListenUDP err[%w]", err))
	}
	s.udpConn = NewUDPConnection(udp)
	var (
		raddr net.Addr
		num   int
	)
	buf := make([]byte, bufferSize)
	//监听start() parser.in的写入
	parser := newParser()
	defer parser.stop()
	// 启动消息处理协程
	go s.handlerListen(parser.out)
	// 主循环：接收UDP数据包
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			num, raddr, err = s.udpConn.ReadFrom(buf)
			if err != nil {
				slog.Error("udp.ReadFromUDP", "err", err)
				continue
			}
			// 解析收到的数据包
			parser.in <- newPacket(append([]byte{}, buf[:num]...), raddr, s.udpConn)
		}
	}
}

// ListenTCPServer 启动 TCP 服务器并监听指定地址。
func (s *Server) ListenTCPServer(addr string) {
	// 解析传入的地址为 TCP 地址
	tcpaddr, err := net.ResolveTCPAddr("tcp", addr)
	// 如果解析地址失败，则抛出错误
	if err != nil {
		panic(fmt.Errorf("net.ResolveUDPAddr err[%w]", err))
	}
	// 保存解析后的 TCP 地址到服务器结构体
	s.tcpaddr = tcpaddr
	// 创建新的端口实例并保存到服务器结构体
	s.tcpPort = NewPort(tcpaddr.Port)

	// 创建 TCP 监听器
	tcp, err := net.ListenTCP("tcp", tcpaddr)
	// 如果创建监听器失败，则抛出错误
	if err != nil {
		panic(fmt.Errorf("net.ListenUDP err[%w]", err))
	}
	// 确保在方法退出时关闭 TCP 监听器
	// 当这个关闭时 所有的设备的socket都会被关闭
	// defer tcp.Close()
	// 保存 TCP 监听器到服务器结构体
	s.tcpListener = tcp
	// 无限循环接受连接

	for {
		select {
		case <-s.ctx.Done():
			slog.Info("ListenTCPServer Has Been Exits")
			return
		default:
			conn, err := tcp.AcceptTCP()
			if err != nil {
				slog.Error("net.ListenTCP", "err", err, "addr", addr)
				return
			}
			go s.ProcessTcpConn(conn)
		}
	}
}

func (s *Server) Close() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.udpConn != nil {
		s.udpConn.Close()
		s.udpConn = nil
	}
	if s.tcpListener != nil {
		s.tcpListener.Close()
		s.tcpListener = nil
	}
}

// ProcessTcpConn 处理传入的 TCP 连接。
func (s *Server) ProcessTcpConn(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	c := NewTCPConnection(conn)

	parser := newParser()
	defer parser.stop()
	go s.handlerListen(parser.out)

	for {
		var buffer bytes.Buffer
		bodyLen := 0
		for {
			// 读取一行数据，以 '\n' 为结束符
			line, err := reader.ReadBytes('\n')
			if err != nil {
				// logrus.Debugln("tcp conn read error:", err)
				return
			}
			buffer.Write(line)
			if len(line) <= 2 {
				if bodyLen <= 0 {
					break
				}

				bodyBuf := make([]byte, bodyLen)
				n, err := io.ReadFull(reader, bodyBuf)
				if err != nil || n != bodyLen {
					slog.Error(`error while read full`, "err", err)
				}
				buffer.Write(bodyBuf)
				break
			}

			if strings.Contains(string(line), "Content-Length") {
				s := strings.Split(string(line), ":")
				value := strings.Trim(s[len(s)-1], " \r\n")
				bodyLen, err = strconv.Atoi(value)
				if err != nil {
					slog.Error("parse Content-Length failed")
					break
				}
			}
		}

		parser.in <- newPacket(buffer.Bytes(), conn.RemoteAddr(), c)
	}
}

// handlerListen 处理接收到的SIP消息
func (s *Server) handlerListen(msgs chan Message) {
	var msg Message
	for {
		msg = <-msgs
		switch tmsg := msg.(type) {
		case *Request:
			// 处理SIP请求消息
			req := tmsg

			dst := s.udpaddr
			if req.conn.Network() == "tcp" {
				dst = s.tcpaddr
			}

			req.SetDestination(dst)
			s.handlerRequest(req)
		case *Response:
			// 处理SIP响应消息
			resp := tmsg

			dst := s.udpaddr
			if resp.conn.Network() == "tcp" {
				dst = s.tcpaddr
			}
			resp.SetDestination(dst)
			s.handlerResponse(resp)
		default:
			// 未知消息类型
		}
	}
}

func (s *Server) handlerRequest(msg *Request) {
	tx := s.mustTX(msg)
	// logrus.Traceln("receive request from:", msg.Source(), ",method:", msg.Method(), "txKey:", tx.key, "message: \n", msg.String())

	key := msg.Method()
	if key == MethodMessage || key == MethodNotify {

		if l, ok := msg.ContentLength(); !ok || l.Equals(0) {
			slog.Error("ContentLength is empty")
			return
		}
		body := msg.Body()
		var msg MessageReceive
		if err := XMLDecode(body, &msg); err != nil {
			slog.Error("xml decode err")
			return
		}
		key += "-" + msg.CmdType
	}
	handlers, ok := s.route.Load(strings.ToUpper(key))
	if !ok {
		slog.Error("not found handler func,string:", "method", msg.Method(), "msg", msg.String())
		go handlerMethodNotAllowed(msg, tx)
		return
	}

	ctx := newContext(msg, tx)
	ctx.handlers = handlers
	ctx.From = s.from
	ctx.svr = s
	go ctx.Next()
}

func (s *Server) handlerResponse(msg *Response) {
	tx := s.getTX(getTXKey(msg))
	if tx == nil {
		// logrus.Infoln("not found tx. receive response from:", msg.Source(), "message: \n", msg.String())
	} else {
		// logrus.Traceln("receive response from:", msg.Source(), "txKey:", tx.key, "message: \n", msg.String())
		tx.receiveResponse(msg)
	}
}

// Request Request
func (s *Server) Request(req *Request) (*Transaction, error) {
	viaHop, ok := req.ViaHop()
	if !ok {
		return nil, fmt.Errorf("missing required 'Via' header")
	}
	viaHop.Host = s.host.String()
	viaHop.Port = s.port
	if viaHop.Params == nil {
		viaHop.Params = NewParams().Add("branch", String{Str: GenerateBranch()})
	}
	if !viaHop.Params.Has("rport") {
		viaHop.Params.Add("rport", nil)
	}

	tx := s.mustTX(req)
	return tx, tx.Request(req)
}

func handlerMethodNotAllowed(req *Request, tx *Transaction) {
	resp := NewResponseFromRequest("", req, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed), []byte{})
	tx.Respond(resp)
}
