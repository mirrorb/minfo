// Package logstream 提供最小依赖的 WebSocket 握手与帧收发实现。

package logstream

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type wsConn struct {
	conn    net.Conn
	reader  *bufio.Reader
	writer  *bufio.Writer
	writeMu sync.Mutex
}

// upgradeWebSocket 完成最小化的 WebSocket 握手，并返回可读写 JSON 帧的连接。
func upgradeWebSocket(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return nil, errors.New("websocket method must be GET")
	}
	if !sameOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, errors.New("websocket origin forbidden")
	}
	if !headerContainsToken(r.Header, "Connection", "Upgrade") || !headerContainsToken(r.Header, "Upgrade", "websocket") {
		http.Error(w, "bad websocket upgrade request", http.StatusBadRequest)
		return nil, errors.New("missing websocket upgrade headers")
	}
	if strings.TrimSpace(r.Header.Get("Sec-WebSocket-Version")) != "13" {
		http.Error(w, "unsupported websocket version", http.StatusBadRequest)
		return nil, errors.New("unsupported websocket version")
	}

	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		http.Error(w, "missing websocket key", http.StatusBadRequest)
		return nil, errors.New("missing websocket key")
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket upgrade unsupported", http.StatusInternalServerError)
		return nil, errors.New("response writer does not support hijacking")
	}

	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}

	accept := computeAcceptKey(key)
	if _, err := fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Accept: %s\r\n\r\n", accept); err != nil {
		conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}

	return &wsConn{
		conn:   conn,
		reader: rw.Reader,
		writer: rw.Writer,
	}, nil
}

// Close 关闭底层网络连接。
func (c *wsConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// WriteJSON 将值编码为 JSON 文本帧并发送给客户端。
func (c *wsConn) WriteJSON(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.writeFrame(0x1, payload)
}

// WriteClose 向客户端发送一个关闭帧。
func (c *wsConn) WriteClose() error {
	return c.writeFrame(0x8, nil)
}

// ReadLoop 持续读取并丢弃客户端帧，直到对端关闭或发生错误。
func (c *wsConn) ReadLoop() error {
	for {
		opcode, err := c.readAndDiscardFrame()
		if err != nil {
			return err
		}
		if opcode == 0x8 {
			return nil
		}
	}
}

// writeFrame 按 WebSocket 帧格式写出一个服务端消息。
func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}

	header := make([]byte, 0, 10)
	header = append(header, 0x80|(opcode&0x0F))
	switch {
	case len(payload) <= 125:
		header = append(header, byte(len(payload)))
	case len(payload) <= 65535:
		header = append(header, 126, byte(len(payload)>>8), byte(len(payload)))
	default:
		header = append(header, 127)
		var extended [8]byte
		binary.BigEndian.PutUint64(extended[:], uint64(len(payload)))
		header = append(header, extended[:]...)
	}

	if _, err := c.writer.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := c.writer.Write(payload); err != nil {
			return err
		}
	}
	return c.writer.Flush()
}

// readAndDiscardFrame 读取一个客户端帧并丢弃其负载，只返回 opcode。
func (c *wsConn) readAndDiscardFrame() (byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(c.reader, header[:]); err != nil {
		return 0, err
	}

	opcode := header[0] & 0x0F
	payloadLen := uint64(header[1] & 0x7F)
	masked := header[1]&0x80 != 0

	switch payloadLen {
	case 126:
		var extended [2]byte
		if _, err := io.ReadFull(c.reader, extended[:]); err != nil {
			return 0, err
		}
		payloadLen = uint64(binary.BigEndian.Uint16(extended[:]))
	case 127:
		var extended [8]byte
		if _, err := io.ReadFull(c.reader, extended[:]); err != nil {
			return 0, err
		}
		payloadLen = binary.BigEndian.Uint64(extended[:])
	}

	if masked {
		var maskKey [4]byte
		if _, err := io.ReadFull(c.reader, maskKey[:]); err != nil {
			return 0, err
		}
	}

	if payloadLen > 0 {
		if _, err := io.CopyN(io.Discard, c.reader, int64(payloadLen)); err != nil {
			return 0, err
		}
	}
	return opcode, nil
}

// computeAcceptKey 按 RFC 6455 规则计算 Sec-WebSocket-Accept。
func computeAcceptKey(key string) string {
	sum := sha1.Sum([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// headerContainsToken 检查指定请求头是否包含某个逗号分隔的令牌值。
func headerContainsToken(header http.Header, key, want string) bool {
	for _, value := range header.Values(key) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), want) {
				return true
			}
		}
	}
	return false
}

// sameOrigin 验证 Origin 是否与请求主机或反向代理转发主机一致。
func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	originURL, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originHost, ok := normalizeOriginHost(originURL)
	if !ok {
		return false
	}
	for _, candidate := range requestHostCandidates(r, originURL.Scheme) {
		if strings.EqualFold(originHost, candidate) {
			return true
		}
		if sameHostname(originHost, candidate) {
			return true
		}
	}
	return false
}

// normalizeOriginHost 会规范化来源主机，并在输入为空或不受支持时返回稳定的默认值。
func normalizeOriginHost(originURL *url.URL) (string, bool) {
	if originURL == nil {
		return "", false
	}
	if originURL.Host == "" {
		return "", false
	}
	return normalizeHostPort(originURL.Host, originURL.Scheme, "")
}

// requestHostCandidates 收集当前请求可能使用的主机名候选，用于同源校验。
func requestHostCandidates(r *http.Request, originScheme string) []string {
	candidates := make([]string, 0, 4)
	seen := make(map[string]struct{})
	forwardedPort := firstForwardedPort(r)

	appendCandidate := func(raw string) {
		normalized, ok := normalizeHostPort(raw, originScheme, forwardedPort)
		if !ok {
			return
		}
		if _, exists := seen[normalized]; exists {
			return
		}
		seen[normalized] = struct{}{}
		candidates = append(candidates, normalized)
	}

	for _, raw := range splitHeaderList(r.Header.Values("X-Forwarded-Host")) {
		appendCandidate(raw)
	}
	if host := firstForwardedHost(r.Header.Values("Forwarded")); host != "" {
		appendCandidate(host)
	}
	appendCandidate(r.Host)
	return candidates
}

// firstForwardedPort 返回转发请求头里声明的第一个端口。
func firstForwardedPort(r *http.Request) string {
	for _, raw := range splitHeaderList(r.Header.Values("X-Forwarded-Port")) {
		port := strings.TrimSpace(raw)
		if port != "" {
			return port
		}
	}
	return ""
}

// firstForwardedHost 从 Forwarded 头中提取第一个 host 参数。
func firstForwardedHost(values []string) string {
	for _, value := range values {
		parts := strings.Split(value, ",")
		for _, part := range parts {
			directives := strings.Split(part, ";")
			for _, directive := range directives {
				name, rawValue, ok := strings.Cut(strings.TrimSpace(directive), "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(name), "host") {
					continue
				}
				host := strings.Trim(strings.TrimSpace(rawValue), "\"")
				if host != "" {
					return host
				}
			}
		}
	}
	return ""
}

// splitHeaderList 按逗号拆分 HTTP 头部列表，并去掉两端空白。
func splitHeaderList(values []string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				items = append(items, part)
			}
		}
	}
	return items
}

// normalizeHostPort 会规范化主机Port，并在输入为空或不受支持时返回稳定的默认值。
func normalizeHostPort(rawHost, scheme, forwardedPort string) (string, bool) {
	host := strings.TrimSpace(rawHost)
	if host == "" {
		return "", false
	}

	host = strings.Trim(host, "\"")
	if strings.Contains(host, "://") {
		parsed, err := url.Parse(host)
		if err != nil {
			return "", false
		}
		host = parsed.Host
	}

	hostname, port, err := net.SplitHostPort(host)
	if err != nil {
		if !strings.Contains(err.Error(), "missing port in address") {
			return "", false
		}
		hostname = host
	}
	hostname = strings.ToLower(strings.Trim(strings.TrimSpace(hostname), "[]"))
	if hostname == "" {
		return "", false
	}

	if port == "" && strings.TrimSpace(forwardedPort) != "" {
		port = strings.TrimSpace(forwardedPort)
	}
	if port == "" {
		switch strings.ToLower(strings.TrimSpace(scheme)) {
		case "https", "wss":
			port = "443"
		case "http", "ws":
			port = "80"
		}
	}
	if port == "" {
		return hostname, true
	}
	return net.JoinHostPort(hostname, port), true
}

// sameHostname 在忽略端口后比较两个主机名是否相同。
func sameHostname(left, right string) bool {
	leftHost, ok := hostOnly(left)
	if !ok {
		return false
	}
	rightHost, ok := hostOnly(right)
	if !ok {
		return false
	}
	return strings.EqualFold(leftHost, rightHost)
}

// hostOnly 返回 host:port 里的主机部分；解析失败时返回 false。
func hostOnly(raw string) (string, bool) {
	host := strings.TrimSpace(raw)
	if host == "" {
		return "", false
	}

	host = strings.Trim(host, "\"")
	if strings.Contains(host, "://") {
		parsed, err := url.Parse(host)
		if err != nil {
			return "", false
		}
		host = parsed.Host
	}

	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		if !strings.Contains(err.Error(), "missing port in address") {
			return "", false
		}
		hostname = host
	}
	hostname = strings.ToLower(strings.Trim(strings.TrimSpace(hostname), "[]"))
	if hostname == "" {
		return "", false
	}
	return hostname, true
}
