package handlers

import (
	"context"
	"sync"
	"time"

	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/coder/websocket"
)

var (
	wsConn *websocket.Conn
	wsMu   sync.Mutex
)

// ConnectNotifyWS starts the persistent WebSocket connection to the notify service.
// addr is the host:port of the notify service (e.g. "notify:3000").
func ConnectNotifyWS(ctx context.Context, addr string) {
	go maintainWSConnection(ctx, addr)
}

// WSConnected returns true if there is currently an active WebSocket connection to notify.
func WSConnected() bool {
	wsMu.Lock()
	defer wsMu.Unlock()
	return wsConn != nil
}

func maintainWSConnection(ctx context.Context, addr string) {
	backoff := 3 * time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		url := "ws://" + addr + "/ws"
		conn, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			log.ErrorFmt("WebSocket dial to notify failed: %v; retrying in %v", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < maxBackoff {
				backoff *= 2
			}
			continue
		}

		log.InfoFmt("WebSocket connected to notify at %s", url)
		backoff = 3 * time.Second

		wsMu.Lock()
		wsConn = conn
		wsMu.Unlock()

		awaitWSClose(ctx, conn)

		wsMu.Lock()
		wsConn = nil
		wsMu.Unlock()

		log.Info("WebSocket connection to notify closed; reconnecting...")
	}
}

// awaitWSClose reads from the connection until it closes.
// Required by coder/websocket to process control frames (ping/pong/close).
func awaitWSClose(ctx context.Context, conn *websocket.Conn) {
	for {
		_, _, err := conn.Read(ctx)
		if err != nil {
			return
		}
	}
}
