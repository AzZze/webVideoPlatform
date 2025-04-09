package sip

import (
	"bytes"
	"fmt"
	"net"

	"github.com/gofrs/uuid"
)

// Request SIP请求结构体，包含消息基本信息、方法和接收者
type Request struct {
	message
	method    string // SIP方法名（如REGISTER, INVITE等）
	recipient *URI   // 接收者的URI
}

// NewRequest 创建新的SIP请求
func NewRequest(
	messID MessageID, // 消息ID
	method string, // SIP方法
	recipient *URI, // 接收者URI
	sipVersion string, // SIP版本
	hdrs []Header, // SIP头部字段
	body []byte, // 消息体
) *Request {
	req := new(Request)
	if messID == "" {
		req.messID = MessageID(uuid.Must(uuid.NewV4()).String())
	} else {
		req.messID = messID
	}
	req.SetSipVersion(sipVersion)
	req.startLine = req.StartLine
	req.headers = newHeaders(hdrs)
	req.SetMethod(method)
	req.SetRecipient(recipient)

	if len(body) != 0 {
		req.SetBody(body, true)
	}
	return req
}

// NewRequestFromResponse 根据响应创建新的请求（通常用于ACK等）
func NewRequestFromResponse(method string, inviteResponse *Response) *Request {
	contact, _ := inviteResponse.Contact()
	ackRequest := NewRequest(
		inviteResponse.MessageID(),
		method,
		contact.Address,
		inviteResponse.SipVersion(),
		[]Header{},
		[]byte{},
	)

	CopyHeaders("Via", inviteResponse, ackRequest)
	viaHop, _ := ackRequest.ViaHop()
	// update branch, 2xx ACK is separate Tx
	viaHop.Params.Add("branch", String{Str: GenerateBranch()})

	if len(inviteResponse.GetHeaders("Route")) > 0 {
		CopyHeaders("Route", inviteResponse, ackRequest)
	} else {
		for _, h := range inviteResponse.GetHeaders("Record-Route") {
			uris := make([]*URI, 0)
			for _, u := range h.(*RecordRouteHeader).Addresses {
				uris = append(uris, u.Clone())
			}
			ackRequest.AppendHeader(&RouteHeader{
				Addresses: uris,
			})
		}
	}

	CopyHeaders("From", inviteResponse, ackRequest)
	CopyHeaders("To", inviteResponse, ackRequest)
	CopyHeaders("Call-ID", inviteResponse, ackRequest)
	cseq, _ := inviteResponse.CSeq()
	cseq.MethodName = method
	cseq.SeqNo++
	ackRequest.AppendHeader(cseq)
	ackRequest.SetSource(inviteResponse.Destination())
	ackRequest.SetDestination(inviteResponse.Source())
	return ackRequest
}

// StartLine 返回请求行（RFC 2361 7.1）
func (req *Request) StartLine() string {
	var buffer bytes.Buffer

	// Every SIP request starts with a Request Line - RFC 2361 7.1.
	buffer.WriteString(
		fmt.Sprintf(
			"%s %s %s",
			req.method,
			req.Recipient(),
			req.SipVersion(),
		),
	)

	return buffer.String()
}

// Method 获取请求方法
func (req *Request) Method() string {
	return req.method
}

// SetMethod 设置请求方法
func (req *Request) SetMethod(method string) {
	req.method = method
}

// Recipient 获取接收者URI
func (req *Request) Recipient() *URI {
	return req.recipient
}

// SetRecipient 设置接收者URI
func (req *Request) SetRecipient(recipient *URI) {
	req.recipient = recipient
}

// IsInvite 判断是否为INVITE请求
func (req *Request) IsInvite() bool {
	return req.Method() == MethodInvite
}

// IsAck 判断是否为ACK请求
func (req *Request) IsAck() bool {
	return req.Method() == MethodACK
}

// IsCancel 判断是否为CANCEL请求
func (req *Request) IsCancel() bool {
	return req.Method() == MethodCancel
}

// Source 获取请求源地址
func (req *Request) Source() net.Addr {
	return req.source
}

// SetSource 设置请求源地址
func (req *Request) SetSource(src net.Addr) {
	req.source = src
}

// Destination 获取请求目标地址
func (req *Request) Destination() net.Addr {
	return req.dest
}

// SetDestination 设置请求目标地址
func (req *Request) SetDestination(dest net.Addr) {
	req.dest = dest
}

func (req *Request) SetConnection(conn Connection) {
	req.conn = conn
}

func (req *Request) GetConnection() Connection {
	return req.conn
}

// Clone 克隆请求对象
func (req *Request) Clone() Message {
	return NewRequest(
		"",
		req.Method(),
		req.Recipient().Clone(),
		req.SipVersion(),
		req.headers.CloneHeaders(),
		req.Body(),
	)
}
