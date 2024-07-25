package nogomobile

import "time"

// interface for websocket and webrtc connection
type WsConn interface {
	SetReadLimit(int64)
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	WriteMessage(messageType int, data []byte) (err error)
	WriteJSON(v interface{}) error
	ReadMessage() (messageType int, data []byte, err error)
	SetPongHandler(func(string) error)
	Close() error
}
