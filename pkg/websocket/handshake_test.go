package websocket

import (
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
)

// TestComputeAcceptKey verifies the accept key calculation using the RFC 6455 Section 1.3 example.
func TestComputeAcceptKey(t *testing.T) {
	const (
		clientKey      = "dGhlIHNhbXBsZSBub25jZQ=="
		expectedAccept = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	)

	actual := ComputeAcceptKey(clientKey)
	if actual != expectedAccept {
		t.Fatalf("ComputeAcceptKey() = %s, want %s", actual, expectedAccept)
	}
}

// TestGenerateClientKey tests random key generation format.
func TestGenerateClientKey(t *testing.T) {
	key, err := GenerateClientKey()
	if err != nil {
		t.Fatalf("GenerateClientKey() error = %v", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		t.Fatalf("GenerateClientKey() returned invalid base64: %v", err)
	}

	if len(decoded) != 16 {
		t.Fatalf("decoded key length = %d, want 16 bytes", len(decoded))
	}
}

// TestServerHandshake_Validation tests error cases during server handshake.
func TestServerHandshake_Validation(t *testing.T) {
	tests := []struct {
		name        string
		request     string
		expectedErr error
	}{
		{
			name: "invalid upgrade header",
			request: "GET / HTTP/1.1\r\n" +
				"Host: localhost\r\n" +
				"Upgrade: other\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
				"Sec-WebSocket-Version: 13\r\n\r\n",
			expectedErr: ErrInvalidUpgrade,
		},
		{
			name: "invalid connection header",
			request: "GET / HTTP/1.1\r\n" +
				"Host: localhost\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: keep-alive\r\n" +
				"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
				"Sec-WebSocket-Version: 13\r\n\r\n",
			expectedErr: ErrInvalidConnection,
		},
		{
			name: "unsupported version",
			request: "GET / HTTP/1.1\r\n" +
				"Host: localhost\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
				"Sec-WebSocket-Version: 8\r\n\r\n",
			expectedErr: ErrUnsupportedVersion,
		},
		{
			name: "missing sec-websocket-key",
			request: "GET / HTTP/1.1\r\n" +
				"Host: localhost\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Version: 13\r\n\r\n",
			expectedErr: ErrMissingWebSocketKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.request)
			var w strings.Builder

			_, err := ServerHandshake(r, &w)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, tt.expectedErr) {
				t.Fatalf("expected error %v, got %v", tt.expectedErr, err)
			}
		})
	}
}

// TestHandshake_Integration tests the full client-server handshake over net.Pipe.
func TestHandshake_Integration(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	var wg sync.WaitGroup
	wg.Add(2)

	var serverErr, clientErr error

	// Run Server Handshake in goroutine
	go func() {
		defer wg.Done()
		_, serverErr = ServerHandshake(serverConn, serverConn)
	}()

	// Run Client Handshake in goroutine
	go func() {
		defer wg.Done()
		_, clientErr = ClientHandshake(clientConn, clientConn, "localhost:8080", "/chat")
	}()

	wg.Wait()

	if serverErr != nil {
		t.Fatalf("ServerHandshake failed: %v", serverErr)
	}
	if clientErr != nil {
		t.Fatalf("ClientHandshake failed: %v", clientErr)
	}
}
