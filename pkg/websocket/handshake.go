package websocket

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	// MagicGUID is the WebSocket GUID specified in RFC 6455 Section 1.3 / 4.2.2.
	MagicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	// WebSocketVersion is the supported WebSocket protocol version (RFC 6455).
	WebSocketVersion = "13"
)

var (
	ErrInvalidUpgrade      = errors.New("websocket: invalid Upgrade header, expected 'websocket'")
	ErrInvalidConnection   = errors.New("websocket: invalid Connection header, expected 'Upgrade'")
	ErrMissingWebSocketKey = errors.New("websocket: missing or empty Sec-WebSocket-Key header")
	ErrUnsupportedVersion  = errors.New("websocket: unsupported Sec-WebSocket-Version, expected 13")
	ErrInvalidResponseCode = errors.New("websocket: invalid handshake response status code")
	ErrInvalidAcceptKey    = errors.New("websocket: invalid Sec-WebSocket-Accept header value")
)

// ComputeAcceptKey computes the Sec-WebSocket-Accept response value
// from a client's Sec-WebSocket-Key as defined in RFC 6455 Section 4.2.2.
func ComputeAcceptKey(clientKey string) string {
	h := sha1.New()
	h.Write([]byte(clientKey + MagicGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// GenerateClientKey generates a cryptographically random 16-byte value
// encoded as base64 for use in Sec-WebSocket-Key.
func GenerateClientKey() (string, error) {
	key := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("generate random key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// ServerHandshake processes an incoming HTTP Upgrade request from r,
// verifies compliance with RFC 6455, writes the 101 Switching Protocols
// response to w, and returns the *bufio.Reader preserving any buffered bytes.
func ServerHandshake(r io.Reader, w io.Writer) (*bufio.Reader, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}

	req, err := http.ReadRequest(br)
	if err != nil {
		return nil, fmt.Errorf("read handshake request: %w", err)
	}

	// 1. Upgrade: websocket
	if !headerContainsToken(req.Header.Get("Upgrade"), "websocket") {
		return nil, ErrInvalidUpgrade
	}

	// 2. Connection: Upgrade
	if !headerContainsToken(req.Header.Get("Connection"), "upgrade") {
		return nil, ErrInvalidConnection
	}

	// 3. Sec-WebSocket-Version: 13
	if req.Header.Get("Sec-WebSocket-Version") != WebSocketVersion {
		return nil, fmt.Errorf("%w: got '%s'", ErrUnsupportedVersion, req.Header.Get("Sec-WebSocket-Version"))
	}

	// 4. Sec-WebSocket-Key
	clientKey := req.Header.Get("Sec-WebSocket-Key")
	if clientKey == "" {
		return nil, ErrMissingWebSocketKey
	}

	// Calculate accept key
	acceptKey := ComputeAcceptKey(clientKey)

	// Send HTTP 101 Switching Protocols response
	resp := fmt.Sprintf(
		"HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: %s\r\n\r\n",
		acceptKey,
	)

	if _, err := io.WriteString(w, resp); err != nil {
		return nil, fmt.Errorf("write handshake response: %w", err)
	}

	return br, nil
}

// ClientHandshake sends a WebSocket HTTP Upgrade request to the given host and path,
// reads and validates the server's 101 Switching Protocols response,
// and returns the *bufio.Reader preserving any buffered bytes.
func ClientHandshake(r io.Reader, w io.Writer, host, path string) (*bufio.Reader, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}

	if path == "" {
		path = "/"
	}

	clientKey, err := GenerateClientKey()
	if err != nil {
		return nil, err
	}

	req := fmt.Sprintf(
		"GET %s HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: %s\r\n\r\n",
		path, host, clientKey, WebSocketVersion,
	)

	if _, err := io.WriteString(w, req); err != nil {
		return nil, fmt.Errorf("write handshake request: %w", err)
	}

	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		return nil, fmt.Errorf("read handshake response: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, fmt.Errorf("%w: status %d", ErrInvalidResponseCode, resp.StatusCode)
	}

	if !headerContainsToken(resp.Header.Get("Upgrade"), "websocket") {
		return nil, ErrInvalidUpgrade
	}

	if !headerContainsToken(resp.Header.Get("Connection"), "upgrade") {
		return nil, ErrInvalidConnection
	}

	expectedAccept := ComputeAcceptKey(clientKey)
	if resp.Header.Get("Sec-WebSocket-Accept") != expectedAccept {
		return nil, ErrInvalidAcceptKey
	}

	return br, nil
}

// headerContainsToken checks if a comma-separated header value contains the target token (case-insensitive).
func headerContainsToken(headerValue, token string) bool {
	for _, part := range strings.Split(headerValue, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}
