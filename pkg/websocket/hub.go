package websocket

import (
	"fmt"
	"io"
	"sync"
)

// DefaultSendBufferSize is the default capacity of each client's send channel.
const DefaultSendBufferSize = 256

// Client represents a connected WebSocket peer managed by a Hub.
type Client struct {
	Hub  *Hub
	Conn io.WriteCloser
	Send chan *Frame
	ID   string

	closeOnce sync.Once
}

// NewClient creates a new Client with a buffered send channel.
func NewClient(conn io.WriteCloser, hub *Hub, id string, bufferSize int) *Client {
	if bufferSize <= 0 {
		bufferSize = DefaultSendBufferSize
	}
	return &Client{
		Hub:  hub,
		Conn: conn,
		Send: make(chan *Frame, bufferSize),
		ID:   id,
	}
}

// Close closes the client's send channel once and unregisters from the Hub.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		if c.Hub != nil {
			c.Hub.Unregister(c)
		}
	})
}

// WritePump pulls frames from the Send channel and writes them sequentially to Conn.
// This guarantees that only one goroutine writes to Conn at any given time.
func (c *Client) WritePump() {
	defer func() {
		_ = c.Conn.Close()
	}()

	for frame := range c.Send {
		if err := WriteFrame(c.Conn, frame); err != nil {
			break
		}
	}
}

// Hub manages active WebSocket client connections and coordinates broadcast messages.
type Hub struct {
	clients   map[*Client]bool
	broadcast chan *Frame
	register  chan *Client
	unregister chan *Client
	quit      chan struct{}

	mu sync.RWMutex
}

// NewHub initializes and returns a new Hub.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan *Frame, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		quit:       make(chan struct{}),
	}
}

// Run starts the Hub's event loop. It should be executed in a dedicated goroutine.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}
			h.mu.Unlock()

		case frame := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				select {
				case client.Send <- frame:
				default:
					// If the send buffer is full, disconnect slow client to prevent blocking
					delete(h.clients, client)
					close(client.Send)
				}
			}
			h.mu.Unlock()

		case <-h.quit:
			h.mu.Lock()
			for client := range h.clients {
				delete(h.clients, client)
				close(client.Send)
			}
			h.mu.Unlock()
			return
		}
	}
}

// Stop stops the Hub and disconnects all clients.
func (h *Hub) Stop() {
	select {
	case <-h.quit:
	default:
		close(h.quit)
	}
}

// Register adds a client to the Hub.
func (h *Hub) Register(c *Client) {
	select {
	case h.register <- c:
	case <-h.quit:
	}
}

// Unregister removes a client from the Hub.
func (h *Hub) Unregister(c *Client) {
	select {
	case h.unregister <- c:
	case <-h.quit:
	}
}

// Broadcast sends a frame to all connected clients.
func (h *Hub) Broadcast(frame *Frame) {
	select {
	case h.broadcast <- frame:
	case <-h.quit:
	}
}

// BroadcastText sends a text frame (unmasked, for server) with the given message to all clients.
func (h *Hub) BroadcastText(msg string) {
	h.Broadcast(NewTextFrame([]byte(msg), false))
}

// ClientCount returns the number of currently active clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ClientIDs returns a list of active client IDs.
func (h *Hub) ClientIDs() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]string, 0, len(h.clients))
	for c := range h.clients {
		ids = append(ids, c.ID)
	}
	return ids
}

// String returns summary information about the Hub.
func (h *Hub) String() string {
	return fmt.Sprintf("Hub(active_clients=%d)", h.ClientCount())
}
