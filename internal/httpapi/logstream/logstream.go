// Package logstream 管理实时日志会话和广播订阅。

package logstream

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const sessionTTL = time.Minute

var (
	defaultHub      = newHub()
	sessionIDRegexp = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

// Event 表示实时日志流中的一条事件。
type Event struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`
	Message   string `json:"message,omitempty"`
}

// Session 表示一个可发布日志并在结束时主动关闭的实时日志会话。
type Session struct {
	hub *hub
	id  string
}

type hub struct {
	mu       sync.Mutex
	sessions map[string]*sessionState
}

type sessionState struct {
	hub         *hub
	id          string
	mu          sync.Mutex
	history     []Event
	subscribers map[*subscriber]struct{}
	closed      bool
	cleanup     *time.Timer
}

type subscriber struct {
	conn *wsConn
	send chan Event
	mu   sync.Mutex
	done bool
}

// NormalizeSessionID 会规范化会话ID，并在输入为空或不受支持时返回稳定的默认值。
func NormalizeSessionID(raw string) string {
	sessionID := strings.TrimSpace(raw)
	if !sessionIDRegexp.MatchString(sessionID) {
		return ""
	}
	return sessionID
}

// Open 返回指定会话 ID 对应的实时日志会话；会话 ID 无效时返回 nil。
func Open(raw string) *Session {
	sessionID := NormalizeSessionID(raw)
	if sessionID == "" {
		return nil
	}
	defaultHub.ensureSession(sessionID)
	return &Session{hub: defaultHub, id: sessionID}
}

// Publish 会广播日志会话，把日志或事件发送给当前订阅方。
func (s *Session) Publish(message string) {
	if s == nil {
		return
	}
	s.hub.publish(s.id, strings.TrimRight(message, "\r\n"))
}

// Publishf 按格式化字符串写入一条日志消息。
func (s *Session) Publishf(format string, args ...any) {
	if s == nil {
		return
	}
	s.Publish(fmt.Sprintf(format, args...))
}

// Close 会关闭日志会话，并释放当前流程持有的资源。
func (s *Session) Close() {
	if s == nil {
		return
	}
	s.hub.close(s.id)
}

// ServeWebSocket 将当前 HTTP 请求升级为日志 WebSocket 连接，并订阅指定会话。
func ServeWebSocket(w http.ResponseWriter, r *http.Request, sessionID string) error {
	conn, err := upgradeWebSocket(w, r)
	if err != nil {
		return err
	}

	sub := &subscriber{
		conn: conn,
		send: make(chan Event, 256),
	}

	history, closed := defaultHub.addSubscriber(sessionID, sub)

	go sub.writeLoop(history, closed)
	go sub.readLoop(sessionID)
	return nil
}

// newHub 创建一个空的日志会话中心。
func newHub() *hub {
	return &hub{sessions: make(map[string]*sessionState)}
}

// ensureSession 获取已存在的会话状态；不存在时创建一个新的。
func (h *hub) ensureSession(sessionID string) *sessionState {
	h.mu.Lock()
	defer h.mu.Unlock()

	if state, ok := h.sessions[sessionID]; ok {
		state.stopCleanup()
		return state
	}

	state := &sessionState{
		hub:         h,
		id:          sessionID,
		history:     make([]Event, 0, 32),
		subscribers: make(map[*subscriber]struct{}),
	}
	h.sessions[sessionID] = state
	return state
}

// publish 会广播日志会话，把日志或事件发送给当前订阅方。
func (h *hub) publish(sessionID, message string) {
	state := h.ensureSession(sessionID)
	event := Event{
		Type:      "line",
		Timestamp: time.Now().Format("15:04:05"),
		Message:   message,
	}

	state.mu.Lock()
	if state.closed {
		state.mu.Unlock()
		return
	}
	state.history = append(state.history, event)
	subscribers := state.snapshotSubscribersLocked()
	state.mu.Unlock()

	for _, sub := range subscribers {
		sub.enqueue(event)
	}
}

// close 会关闭日志会话，并释放当前流程持有的资源。
func (h *hub) close(sessionID string) {
	h.mu.Lock()
	state, ok := h.sessions[sessionID]
	h.mu.Unlock()
	if !ok {
		return
	}

	event := Event{
		Type:      "end",
		Timestamp: time.Now().Format("15:04:05"),
	}

	state.mu.Lock()
	if state.closed {
		state.mu.Unlock()
		return
	}
	state.closed = true
	state.history = append(state.history, event)
	subscribers := state.snapshotSubscribersLocked()
	state.subscribers = make(map[*subscriber]struct{})
	state.mu.Unlock()

	for _, sub := range subscribers {
		sub.enqueue(event)
		sub.closeSend()
	}

	state.scheduleCleanup()
}

// addSubscriber 将订阅者附加到会话，并返回当前历史记录以及会话是否已关闭。
func (h *hub) addSubscriber(sessionID string, sub *subscriber) ([]Event, bool) {
	state := h.ensureSession(sessionID)

	state.mu.Lock()
	defer state.mu.Unlock()

	history := append([]Event(nil), state.history...)
	if state.closed {
		return history, true
	}

	state.subscribers[sub] = struct{}{}
	return history, false
}

// removeSubscriber 从会话中移除一个订阅者；必要时触发延迟清理。
func (h *hub) removeSubscriber(sessionID string, sub *subscriber) {
	h.mu.Lock()
	state, ok := h.sessions[sessionID]
	h.mu.Unlock()
	if !ok {
		return
	}

	state.mu.Lock()
	delete(state.subscribers, sub)
	closed := state.closed
	remaining := len(state.subscribers)
	state.mu.Unlock()

	if closed && remaining == 0 {
		state.scheduleCleanup()
	}
}

// snapshotSubscribersLocked 复制当前订阅者列表；调用方必须先持有 sessionState 的锁。
func (s *sessionState) snapshotSubscribersLocked() []*subscriber {
	subscribers := make([]*subscriber, 0, len(s.subscribers))
	for sub := range s.subscribers {
		subscribers = append(subscribers, sub)
	}
	return subscribers
}

// scheduleCleanup 在会话关闭后安排一次延迟清理，避免断线重连立即丢失历史记录。
func (s *sessionState) scheduleCleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cleanup != nil {
		s.cleanup.Stop()
	}
	s.cleanup = time.AfterFunc(sessionTTL, func() {
		s.hub.mu.Lock()
		defer s.hub.mu.Unlock()
		if current, ok := s.hub.sessions[s.id]; ok && current == s {
			delete(s.hub.sessions, s.id)
		}
	})
}

// stopCleanup 取消已经安排的延迟清理任务。
func (s *sessionState) stopCleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cleanup != nil {
		s.cleanup.Stop()
		s.cleanup = nil
	}
}

// enqueue 会把日志会话加入发送队列，等待后续异步写出。
func (s *subscriber) enqueue(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.done {
		return
	}
	select {
	case s.send <- event:
	default:
		close(s.send)
		s.done = true
	}
}

// closeSend 关闭订阅者的发送通道，并确保该操作只执行一次。
func (s *subscriber) closeSend() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.done {
		return
	}
	close(s.send)
	s.done = true
}

// writeLoop 先回放历史事件，再持续把新事件写入 WebSocket 连接。
func (s *subscriber) writeLoop(history []Event, closeAfterHistory bool) {
	defer s.conn.Close()

	for _, event := range history {
		if err := s.writeEvent(event); err != nil {
			s.closeSend()
			return
		}
	}
	if closeAfterHistory {
		_ = s.conn.WriteClose()
		return
	}

	for event := range s.send {
		if err := s.writeEvent(event); err != nil {
			s.closeSend()
			return
		}
	}
	_ = s.conn.WriteClose()
}

// readLoop 持续读取客户端帧，直到连接断开后将订阅者从会话中移除。
func (s *subscriber) readLoop(sessionID string) {
	defer defaultHub.removeSubscriber(sessionID, s)
	defer s.closeSend()
	defer s.conn.Close()

	_ = s.conn.ReadLoop()
}

// writeEvent 将单条事件编码成 JSON 并写入当前 WebSocket 连接。
func (s *subscriber) writeEvent(event Event) error {
	return s.conn.WriteJSON(event)
}
