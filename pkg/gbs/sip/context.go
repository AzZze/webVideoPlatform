package sip

import (
	"fmt"
	"log/slog"
	"math"
	"net"
	"strings"
)

const abortIndex int8 = math.MaxInt8 >> 1

type HandlerFunc func(*Context)

// Context SIP请求处理的上下文结构体
type Context struct {
	Request  *Request      // SIP请求对象
	Tx       *Transaction  // 事务对象
	handlers []HandlerFunc // 处理函数列表
	index    int8          // 当前执行的处理函数索引

	cache map[string]any // 缓存数据

	DeviceID string // 设备ID
	Host     string // 主机地址
	Port     string // 端口号

	Source net.Addr // 源地址
	To     *Address // 目标地址
	From   *Address // 来源地址

	Log *slog.Logger // 日志记录器

	svr *Server // SIP服务器实例
}

// newContext 创建新的上下文实例
func newContext(req *Request, tx *Transaction) *Context {
	c := Context{
		Request: req,
		Tx:      tx,
		cache:   make(map[string]any),
		Log:     slog.Default(),
		index:   -1,
	}
	if err := c.parserRequest(); err != nil {
		slog.Error("parserRequest", "err", err)
	}
	return &c
}

// parserRequest 解析SIP请求，提取关键信息
func (c *Context) parserRequest() error {
	req := c.Request
	header, ok := req.From()
	if !ok {
		return fmt.Errorf("req from is nil")
	}

	if header.Address == nil {
		return fmt.Errorf("header address is nil")
	}
	user := header.Address.User()
	if user == nil {
		return fmt.Errorf("address user is nil")
	}
	c.DeviceID = user.String()
	c.Host = header.Address.Host()
	via, ok := req.ViaHop()
	if !ok {
		return fmt.Errorf("via is nil")
	}
	c.Host = via.Host
	c.Port = via.Port.String()

	c.Source = req.Source()
	c.To = NewAddressFromFromHeader(header)

	c.Log = slog.Default().With("deviceID", c.DeviceID, "host", c.Host)
	return nil
}

// Next 执行下一个处理函数
func (c *Context) Next() {
	c.index++
	for c.index < int8(len(c.handlers)) {
		if fn := c.handlers[c.index]; fn != nil {
			fn(c)
		}
		c.index++
	}
}

// GetHeader 获取指定头部字段的值
func (c *Context) GetHeader(key string) string {
	headers := c.Request.GetHeaders(key)
	if len(headers) > 0 {
		header := headers[0]
		splits := strings.Split(header.String(), ":")
		if len(splits) == 2 {
			return strings.TrimSpace(splits[1])
		}
	}
	return ""
}

// Abort 中止处理流程
func (c *Context) Abort() {
	c.index = abortIndex
}

// AbortString 中止处理并返回错误信息
func (c *Context) AbortString(status int, msg string) {
	c.Abort()
	c.String(status, msg)
}

// String 发送文本响应
func (c *Context) String(status int, msg string) {
	_ = c.Tx.Respond(NewResponseFromRequest("", c.Request, status, msg, nil))
}

// Set 设置上下文缓存数据
func (c *Context) Set(k string, v any) {
	c.cache[k] = v
}

// Get 获取上下文缓存数据
func (c *Context) Get(k string) (any, bool) {
	v, ok := c.cache[k]
	return v, ok
}

// GetMustString 获取字符串类型的缓存数据
func (c *Context) GetMustString(k string) string {
	if v, ok := c.cache[k]; ok {
		return v.(string)
	}
	return ""
}

// GetMustInt 获取整数类型的缓存数据
func (c *Context) GetMustInt(k string) int {
	if v, ok := c.cache[k]; ok {
		return v.(int)
	}
	return 0
}

// SendRequest 发送SIP请求
func (c *Context) SendRequest(method string, body []byte) (*Transaction, error) {
	hb := NewHeaderBuilder().SetTo(c.To).SetFrom(c.From).AddVia(&ViaHop{
		Params: NewParams().Add("branch", String{Str: GenerateBranch()}),
	}).SetContentType(&ContentTypeXML).SetMethod(method)

	req := NewRequest("", method, c.To.URI, DefaultSipVersion, hb.Build(), body)
	req.SetDestination(c.Source)
	req.SetConnection(c.Request.conn)
	return c.svr.Request(req)
}
