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

type Event struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`
	Message   string `json:"message,omitempty"`
}

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

func NormalizeSessionID(raw string) string {
	sessionID := strings.TrimSpace(raw)
	if !sessionIDRegexp.MatchString(sessionID) {
		return ""
	}
	return sessionID
}

func Open(raw string) *Session {
	sessionID := NormalizeSessionID(raw)
	if sessionID == "" {
		return nil
	}
	defaultHub.ensureSession(sessionID)
	return &Session{hub: defaultHub, id: sessionID}
}

func (s *Session) Publish(message string) {
	if s == nil {
		return
	}
	s.hub.publish(s.id, strings.TrimRight(message, "\r\n"))
}

func (s *Session) Publishf(format string, args ...any) {
	if s == nil {
		return
	}
	s.Publish(fmt.Sprintf(format, args...))
}

func (s *Session) Close() {
	if s == nil {
		return
	}
	s.hub.close(s.id)
}

func ServeWS(w http.ResponseWriter, r *http.Request, sessionID string) error {
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

func newHub() *hub {
	return &hub{sessions: make(map[string]*sessionState)}
}

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

func (s *sessionState) snapshotSubscribersLocked() []*subscriber {
	subscribers := make([]*subscriber, 0, len(s.subscribers))
	for sub := range s.subscribers {
		subscribers = append(subscribers, sub)
	}
	return subscribers
}

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

func (s *sessionState) stopCleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cleanup != nil {
		s.cleanup.Stop()
		s.cleanup = nil
	}
}

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

func (s *subscriber) closeSend() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.done {
		return
	}
	close(s.send)
	s.done = true
}

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

func (s *subscriber) readLoop(sessionID string) {
	defer defaultHub.removeSubscriber(sessionID, s)
	defer s.closeSend()
	defer s.conn.Close()

	_ = s.conn.ReadLoop()
}

func (s *subscriber) writeEvent(event Event) error {
	return s.conn.WriteJSON(event)
}
