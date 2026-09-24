package websocket

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// mockConn is an in-memory WriteCloser for testing.
type mockConn struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func (m *mockConn) Write(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf.Write(p)
}

func (m *mockConn) Close() error {
	return nil
}

// TestHub_RegisterUnregister verifies client registration and unregistration.
func TestHub_RegisterUnregister(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	client1 := NewClient(&mockConn{}, hub, "c1", 10)
	client2 := NewClient(&mockConn{}, hub, "c2", 10)

	hub.Register(client1)
	hub.Register(client2)

	// Wait briefly for event loop processing
	time.Sleep(20 * time.Millisecond)
	if count := hub.ClientCount(); count != 2 {
		t.Fatalf("expected 2 clients, got %d", count)
	}

	hub.Unregister(client1)
	time.Sleep(20 * time.Millisecond)
	if count := hub.ClientCount(); count != 1 {
		t.Fatalf("expected 1 client, got %d", count)
	}

	hub.Unregister(client2)
	time.Sleep(20 * time.Millisecond)
	if count := hub.ClientCount(); count != 0 {
		t.Fatalf("expected 0 clients, got %d", count)
	}
}

// TestHub_Broadcast verifies broadcast message delivery to all clients.
func TestHub_Broadcast(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	const clientCount = 3
	var pipesClient [clientCount]net.Conn
	var pipesServer [clientCount]net.Conn

	for i := 0; i < clientCount; i++ {
		pipesClient[i], pipesServer[i] = net.Pipe()
		defer func(c net.Conn) { _ = c.Close() }(pipesClient[i])
		defer func(s net.Conn) { _ = s.Close() }(pipesServer[i])

		client := NewClient(pipesServer[i], hub, fmt.Sprintf("client-%d", i), 10)
		hub.Register(client)
		go client.WritePump()
	}

	time.Sleep(20 * time.Millisecond)
	if hub.ClientCount() != clientCount {
		t.Fatalf("expected %d clients registered, got %d", clientCount, hub.ClientCount())
	}

	// Broadcast message
	const broadcastMsg = "Server Push Notice!"
	hub.BroadcastText(broadcastMsg)

	// Each client should receive the broadcast frame
	for i := 0; i < clientCount; i++ {
		frame, err := ReadFrame(pipesClient[i])
		if err != nil {
			t.Fatalf("client %d failed to read broadcast frame: %v", i, err)
		}
		if frame.Opcode != OpcodeText {
			t.Errorf("client %d expected OpcodeText, got 0x%X", i, frame.Opcode)
		}
		if string(frame.Payload) != broadcastMsg {
			t.Errorf("client %d expected %q, got %q", i, broadcastMsg, string(frame.Payload))
		}
	}
}

// TestHub_SlowClientEviction verifies that full send buffer clients are evicted without blocking others.
func TestHub_SlowClientEviction(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	// Slow client with buffer size 1 and no reader
	slowConn := &mockConn{}
	slowClient := NewClient(slowConn, hub, "slow", 1)
	hub.Register(slowClient)

	// Normal client
	fastPipeClient, fastPipeServer := net.Pipe()
	defer func() { _ = fastPipeClient.Close() }()
	defer func() { _ = fastPipeServer.Close() }()

	fastClient := NewClient(fastPipeServer, hub, "fast", 10)
	hub.Register(fastClient)
	go fastClient.WritePump()

	time.Sleep(20 * time.Millisecond)

	// Fill buffer and trigger eviction on slow client
	hub.BroadcastText("msg 1")
	hub.BroadcastText("msg 2") // Fills buffer
	hub.BroadcastText("msg 3") // Drops slow client

	// Fast client should still receive messages
	for i := 0; i < 3; i++ {
		frame, err := ReadFrame(fastPipeClient)
		if err != nil {
			t.Fatalf("fast client failed to receive frame: %v", err)
		}
		if frame.Opcode != OpcodeText {
			t.Errorf("expected OpcodeText, got %v", frame.Opcode)
		}
	}

	time.Sleep(30 * time.Millisecond)
	// Slow client should be evicted
	if hub.ClientCount() != 1 {
		t.Errorf("expected 1 active client (fast client), got %d", hub.ClientCount())
	}
}

// TestHub_Stop verifies that Stop closes all client send channels and terminates cleanly.
func TestHub_Stop(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	client := NewClient(&mockConn{}, hub, "c1", 10)
	hub.Register(client)
	time.Sleep(20 * time.Millisecond)

	hub.Stop()
	time.Sleep(20 * time.Millisecond)

	// Client send channel should be closed
	select {
	case _, ok := <-client.Send:
		if ok {
			t.Fatalf("expected client Send channel to be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timed out waiting for client Send channel closure")
	}
}

// Ensure mockConn satisfies io.WriteCloser
var _ io.WriteCloser = (*mockConn)(nil)
