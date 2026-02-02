package ws

import (
	"sync"

	"github.com/fasthttp/websocket"
)

type SafeWebSocket struct {
	Conn *websocket.Conn
	mu   sync.Mutex
}

func (s *SafeWebSocket) WriteJSON(v interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Conn.WriteJSON(v)
}

func (s *SafeWebSocket) WriteMessage(msgType int, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Conn.WriteMessage(msgType, data)
}

func (s *SafeWebSocket) Ping() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Conn.WriteMessage(websocket.PingMessage, nil)
}
