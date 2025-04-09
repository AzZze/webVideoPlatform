package sip

import (
	"bufio"
	"bytes"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"
)

// Packet Packet
type Packet struct {
	reader     *bufio.Reader // 用于读取数据的缓冲读取器
	raddr      net.Addr      // 远程地址
	bodylength int           // 消息体长度
	conn       Connection    // 网络连接实例
}

func newPacket(data []byte, raddr net.Addr, conn Connection) Packet {
	slog.Debug("receive new packet,from:", "raddr", raddr.String(), "data", string(data))
	return Packet{
		reader:     bufio.NewReader(bytes.NewReader(data)),
		raddr:      raddr,
		bodylength: getBodyLength(data),
		conn:       conn,
	}
}

func (p *Packet) nextLine() (string, error) {
	str, err := p.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	// Trim the newline characters
	str = strings.TrimSuffix(str, "\r\n")
	str = strings.TrimSuffix(str, "\n")
	return str, nil
}

func (p *Packet) bodyLength() int {
	return p.bodylength
}

func (p *Packet) getBody() ([]byte, error) {
	if p.bodyLength() < 1 {
		return []byte{}, nil
	}
	body := make([]byte, p.bodylength)
	if p.bodylength > 0 {
		n, err := io.ReadFull(p.reader, body)
		if err != nil && err != io.ErrUnexpectedEOF {
			return body, err
		}
		if n != p.bodylength {
			// logrus.Warningf("body length err,%d!=%d,body:%s", n, p.bodylength, string(body))
			return body[:n], nil
		}
	}
	return body, nil
}

// Connection Wrapper around net.Conn.
type Connection interface {
	net.Conn
	Network() string
	// String() string
	ReadFrom(buf []byte) (num int, raddr net.Addr, err error) // 从连接读取数据
	WriteTo(buf []byte, raddr net.Addr) (num int, err error)  // 向连接写入数据
}

// connection 连接实现结构体
type connection struct {
	baseConn net.Conn // 底层网络连接
	laddr    net.Addr // 本地地址
	raddr    net.Addr // 远程地址
	logKey   string   // 日志标识符
}

func NewUDPConnection(baseConn net.Conn) Connection {
	conn := &connection{
		baseConn: baseConn,
		laddr:    baseConn.LocalAddr(),
		raddr:    baseConn.RemoteAddr(),
		logKey:   "udp ",
	}
	return conn
}

func NewTCPConnection(baseConn net.Conn) Connection {
	conn := &connection{
		baseConn: baseConn,
		laddr:    baseConn.LocalAddr(),
		raddr:    baseConn.RemoteAddr(),
		logKey:   "tcp ",
	}
	return conn
}

// Read 从连接读取数据
func (conn *connection) Read(buf []byte) (int, error) {
	var (
		num int
		err error
	)

	num, err = conn.baseConn.Read(buf)
	if err != nil {
		return num, NewError(err, conn.logKey, "read", conn.baseConn.LocalAddr().String())
	}
	return num, err
}

// ReadFrom 从指定地址读取数据
func (conn *connection) ReadFrom(buf []byte) (num int, raddr net.Addr, err error) {
	num, raddr, err = conn.baseConn.(net.PacketConn).ReadFrom(buf)
	if err != nil {
		return num, raddr, NewError(err, conn.logKey, "readfrom", conn.baseConn.LocalAddr().String(), raddr.String())
	}
	// logrus.Tracef("readFrom %d , %s -> %s \n %s", num, raddr, conn.LocalAddr(), string(buf[:num]))
	return num, raddr, err
}

// Write 向连接写入数据
func (conn *connection) Write(buf []byte) (int, error) {
	var (
		num int
		err error
	)

	num, err = conn.baseConn.Write(buf)
	if err != nil {
		return num, NewError(err, conn.logKey, "write", conn.baseConn.LocalAddr().String())
	}
	return num, err
}

// WriteTo 向指定地址写入数据
func (conn *connection) WriteTo(buf []byte, raddr net.Addr) (num int, err error) {
	if conn.Network() == "tcp" {
		num, err = conn.baseConn.Write(buf)
	} else {
		num, err = conn.baseConn.(net.PacketConn).WriteTo(buf, raddr)
	}
	if err != nil {
		return num, NewError(err, conn.logKey, "writeTo", conn.baseConn.LocalAddr().String(), raddr.String())
	}
	// logrus.Tracef("writeTo %d , %s -> %s \n %s", num, conn.baseConn.LocalAddr(), raddr.String(), string(buf[:num]))
	return num, err
}

// LocalAddr 获取本地地址
func (conn *connection) LocalAddr() net.Addr {
	return conn.baseConn.LocalAddr()
}

// RemoteAddr 获取远程地址
func (conn *connection) RemoteAddr() net.Addr {
	return conn.baseConn.RemoteAddr()
}

// Close 关闭连接
func (conn *connection) Close() error {
	err := conn.baseConn.Close()
	if err != nil {
		return NewError(err, conn.logKey, "close", conn.baseConn.LocalAddr().String(), conn.baseConn.RemoteAddr().String())
	}
	return nil
}

// Network 获取网络类型
func (conn *connection) Network() string {
	return conn.baseConn.LocalAddr().Network()
}

// SetDeadline 设置连接的读写超时时间
func (conn *connection) SetDeadline(t time.Time) error {
	return conn.baseConn.SetDeadline(t)
}

// SetReadDeadline 设置连接的读取超时时间
func (conn *connection) SetReadDeadline(t time.Time) error {
	return conn.baseConn.SetReadDeadline(t)
}

// SetWriteDeadline 设置连接的写入超时时间
func (conn *connection) SetWriteDeadline(t time.Time) error {
	return conn.baseConn.SetWriteDeadline(t)
}
