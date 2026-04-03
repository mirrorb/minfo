package logstream

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestServeWSDeliversLiveEvents(t *testing.T) {
	sessionID := fmt.Sprintf("test-live-%d", time.Now().UnixNano())
	server := newLogStreamTestServer(t)
	defer server.Close()

	conn := dialLogStream(t, server.URL, sessionID)
	defer conn.Close()

	session := Open(sessionID)
	if session == nil {
		t.Fatalf("expected session for %q", sessionID)
	}

	session.Publish("hello live log")
	session.Close()

	assertNextEvent(t, conn, "line", "hello live log")
	assertNextEvent(t, conn, "end", "")
}

func TestServeWSReplaysHistoryAfterSessionClosed(t *testing.T) {
	sessionID := fmt.Sprintf("test-history-%d", time.Now().UnixNano())
	session := Open(sessionID)
	if session == nil {
		t.Fatalf("expected session for %q", sessionID)
	}
	session.Publish("history line")
	session.Close()

	server := newLogStreamTestServer(t)
	defer server.Close()

	conn := dialLogStream(t, server.URL, sessionID)
	defer conn.Close()

	assertNextEvent(t, conn, "line", "history line")
	assertNextEvent(t, conn, "end", "")
}

func newLogStreamTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionID := NormalizeSessionID(r.URL.Query().Get("session"))
		if sessionID == "" {
			t.Fatalf("unexpected invalid session id")
		}
		if err := ServeWS(w, r, sessionID); err != nil {
			t.Fatalf("ServeWS failed: %v", err)
		}
	}))
}

type testWSConn struct {
	conn   net.Conn
	reader *bufio.Reader
}

func dialLogStream(t *testing.T, serverURL, sessionID string) *testWSConn {
	t.Helper()

	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse server url failed: %v", err)
	}

	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatalf("dial websocket failed: %v", err)
	}

	reader := bufio.NewReader(conn)
	key := base64.StdEncoding.EncodeToString([]byte("test-websocket-key"))
	path := "/?session=" + sessionID
	if _, err := fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n\r\n", path, parsed.Host, key); err != nil {
		conn.Close()
		t.Fatalf("write websocket handshake failed: %v", err)
	}

	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		conn.Close()
		t.Fatalf("read websocket handshake failed: %v", err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		conn.Close()
		t.Fatalf("unexpected handshake status: %d body=%s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	response.Body.Close()

	return &testWSConn{conn: conn, reader: reader}
}

func (c *testWSConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func assertNextEvent(t *testing.T, conn *testWSConn, wantType, wantMessage string) {
	t.Helper()

	_ = conn.conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	event, err := conn.ReadEvent()
	if err != nil {
		t.Fatalf("read event failed: %v", err)
	}
	if event.Type != wantType {
		t.Fatalf("unexpected event type: got %q want %q", event.Type, wantType)
	}
	if wantType == "line" && event.Message != wantMessage {
		t.Fatalf("unexpected event message: got %q want %q", event.Message, wantMessage)
	}
}

func (c *testWSConn) ReadEvent() (Event, error) {
	payload, err := c.readTextFrame()
	if err != nil {
		return Event{}, err
	}

	var event Event
	if err := json.Unmarshal(payload, &event); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (c *testWSConn) readTextFrame() ([]byte, error) {
	for {
		var header [2]byte
		if _, err := io.ReadFull(c.reader, header[:]); err != nil {
			return nil, err
		}

		opcode := header[0] & 0x0F
		payloadLen := uint64(header[1] & 0x7F)
		masked := header[1]&0x80 != 0

		switch payloadLen {
		case 126:
			var extended [2]byte
			if _, err := io.ReadFull(c.reader, extended[:]); err != nil {
				return nil, err
			}
			payloadLen = uint64(extended[0])<<8 | uint64(extended[1])
		case 127:
			var extended [8]byte
			if _, err := io.ReadFull(c.reader, extended[:]); err != nil {
				return nil, err
			}
			payloadLen = 0
			for _, b := range extended {
				payloadLen = (payloadLen << 8) | uint64(b)
			}
		}

		if masked {
			var maskKey [4]byte
			if _, err := io.ReadFull(c.reader, maskKey[:]); err != nil {
				return nil, err
			}
		}

		payload := make([]byte, payloadLen)
		if payloadLen > 0 {
			if _, err := io.ReadFull(c.reader, payload); err != nil {
				return nil, err
			}
		}

		switch opcode {
		case 0x1:
			return payload, nil
		case 0x8:
			return nil, io.EOF
		default:
			continue
		}
	}
}
