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

func (c *wsConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *wsConn) WriteJSON(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.writeFrame(0x1, payload)
}

func (c *wsConn) WriteClose() error {
	return c.writeFrame(0x8, nil)
}

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

func computeAcceptKey(key string) string {
	sum := sha1.Sum([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

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

func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	originURL, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(originURL.Host, r.Host)
}
