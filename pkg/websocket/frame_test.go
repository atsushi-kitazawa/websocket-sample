package websocket

import (
	"bytes"
	"errors"
	"testing"
)

// TestFrame_RoundTrip verifies that WriteFrame followed by ReadFrame preserves all frame data.
func TestFrame_RoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		frame   *Frame
		masked  bool
		payload []byte
	}{
		{
			name:    "small text frame unmasked",
			frame:   NewTextFrame([]byte("Hello, WebSocket!"), false),
			masked:  false,
			payload: []byte("Hello, WebSocket!"),
		},
		{
			name: "small text frame masked",
			frame: &Frame{
				Fin:        true,
				Opcode:     OpcodeText,
				Masked:     true,
				MaskingKey: [4]byte{0x12, 0x34, 0x56, 0x78},
				Payload:    []byte("Masked Client Message"),
			},
			masked:  true,
			payload: []byte("Masked Client Message"),
		},
		{
			name:    "binary frame unmasked",
			frame:   NewBinaryFrame([]byte{0x00, 0xFF, 0x42, 0x99}, false),
			masked:  false,
			payload: []byte{0x00, 0xFF, 0x42, 0x99},
		},
		{
			name:    "empty ping frame unmasked",
			frame:   NewPingFrame(nil, false),
			masked:  false,
			payload: nil,
		},
		{
			name:    "ping frame with payload masked",
			frame:   NewPingFrame([]byte("heartbeat"), true),
			masked:  true,
			payload: []byte("heartbeat"),
		},
		{
			name:    "pong frame unmasked",
			frame:   NewPongFrame([]byte("heartbeat"), false),
			masked:  false,
			payload: []byte("heartbeat"),
		},
		{
			name:    "close frame with code and reason",
			frame:   NewCloseFrame(CloseNormalClosure, "bye", false),
			masked:  false,
			payload: []byte{0x03, 0xE8, 'b', 'y', 'e'},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteFrame(&buf, tt.frame); err != nil {
				t.Fatalf("WriteFrame failed: %v", err)
			}

			readBack, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame failed: %v", err)
			}

			if readBack.Fin != tt.frame.Fin {
				t.Errorf("Fin mismatch: got %v, want %v", readBack.Fin, tt.frame.Fin)
			}
			if readBack.Opcode != tt.frame.Opcode {
				t.Errorf("Opcode mismatch: got %v, want %v", readBack.Opcode, tt.frame.Opcode)
			}
			if readBack.Masked != tt.masked {
				t.Errorf("Masked mismatch: got %v, want %v", readBack.Masked, tt.masked)
			}
			if !bytes.Equal(readBack.Payload, tt.payload) {
				t.Errorf("Payload mismatch: got %q, want %q", readBack.Payload, tt.payload)
			}
		})
	}
}

// TestPayloadLengthBoundaries tests encoding and decoding around payload boundary values:
// 0, 125, 126, 65535, 65536.
func TestPayloadLengthBoundaries(t *testing.T) {
	lengths := []int{
		0,      // Empty payload
		125,    // Max 7-bit length
		126,    // Min 16-bit extended length
		1024,   // Intermediate length
		65535,  // Max 16-bit extended length
		65536,  // Min 64-bit extended length
	}

	for _, length := range lengths {
		t.Run(string(rune(length)), func(t *testing.T) {
			payload := make([]byte, length)
			for i := range payload {
				payload[i] = byte(i % 256)
			}

			// Test both unmasked (server) and masked (client)
			for _, masked := range []bool{false, true} {
				frame := &Frame{
					Fin:     true,
					Opcode:  OpcodeBinary,
					Masked:  masked,
					Payload: payload,
				}

				var buf bytes.Buffer
				if err := WriteFrame(&buf, frame); err != nil {
					t.Fatalf("WriteFrame (len=%d, masked=%v) failed: %v", length, masked, err)
				}

				decoded, err := ReadFrame(&buf)
				if err != nil {
					t.Fatalf("ReadFrame (len=%d, masked=%v) failed: %v", length, masked, err)
				}

				if len(decoded.Payload) != length {
					t.Fatalf("Payload length mismatch: got %d, want %d", len(decoded.Payload), length)
				}
				if !bytes.Equal(decoded.Payload, payload) {
					t.Fatalf("Payload content mismatch at len=%d", length)
				}
			}
		})
	}
}

// TestMaskPayload tests XOR masking symmetry and correct masking.
func TestMaskPayload(t *testing.T) {
	original := []byte("The quick brown fox jumps over the lazy dog")
	key := [4]byte{0xDE, 0xAD, 0xBE, 0xEF}

	buf := make([]byte, len(original))
	copy(buf, original)

	// Apply mask
	MaskPayload(buf, key)
	if bytes.Equal(buf, original) {
		t.Fatal("MaskPayload should alter original bytes")
	}

	// Unmask
	MaskPayload(buf, key)
	if !bytes.Equal(buf, original) {
		t.Fatalf("Unmasked bytes do not match original: got %q, want %q", buf, original)
	}
}

// TestCloseFrameParsing tests parsing close code and reason.
func TestCloseFrameParsing(t *testing.T) {
	// Normal close
	frame := NewCloseFrame(CloseNormalClosure, "normal shutdown", false)
	code, reason, err := frame.ParseClosePayload()
	if err != nil {
		t.Fatalf("ParseClosePayload failed: %v", err)
	}
	if code != CloseNormalClosure {
		t.Errorf("code mismatch: got %d, want %d", code, CloseNormalClosure)
	}
	if reason != "normal shutdown" {
		t.Errorf("reason mismatch: got %q, want 'normal shutdown'", reason)
	}

	// Empty close
	emptyFrame := NewCloseFrame(0, "", false)
	code, reason, err = emptyFrame.ParseClosePayload()
	if err != nil {
		t.Fatalf("ParseClosePayload on empty close failed: %v", err)
	}
	if code != 0 || reason != "" {
		t.Errorf("expected 0/'', got %d/%q", code, reason)
	}

	// Invalid 1-byte close payload
	invalidFrame := &Frame{
		Fin:     true,
		Opcode:  OpcodeClose,
		Payload: []byte{0x03},
	}
	_, _, err = invalidFrame.ParseClosePayload()
	if !errors.Is(err, ErrInvalidClosePayload) {
		t.Fatalf("expected ErrInvalidClosePayload, got %v", err)
	}
}

// TestFrameErrors tests RFC 6455 error conditions during frame parsing.
func TestFrameErrors(t *testing.T) {
	t.Run("reserved bits non zero", func(t *testing.T) {
		// RSV1 is set (0x40 in first byte)
		raw := []byte{0xC1, 0x00}
		_, err := ReadFrame(bytes.NewReader(raw))
		if !errors.Is(err, ErrReservedBitsNonZero) {
			t.Fatalf("expected ErrReservedBitsNonZero, got %v", err)
		}
	})

	t.Run("invalid opcode", func(t *testing.T) {
		// Opcode 0x3 (undefined)
		raw := []byte{0x83, 0x00}
		_, err := ReadFrame(bytes.NewReader(raw))
		if !errors.Is(err, ErrInvalidOpcode) {
			t.Fatalf("expected ErrInvalidOpcode, got %v", err)
		}
	})

	t.Run("control frame fragmented", func(t *testing.T) {
		// Ping frame (0x9) with FIN=0 (0x09 instead of 0x89)
		raw := []byte{0x09, 0x00}
		_, err := ReadFrame(bytes.NewReader(raw))
		if !errors.Is(err, ErrControlFrameFragmented) {
			t.Fatalf("expected ErrControlFrameFragmented, got %v", err)
		}
	})

	t.Run("control frame too large", func(t *testing.T) {
		// Ping frame (0x89) with payload length 126
		raw := []byte{0x89, 126, 0x00, 126}
		_, err := ReadFrame(bytes.NewReader(raw))
		if !errors.Is(err, ErrControlFrameTooLarge) {
			t.Fatalf("expected ErrControlFrameTooLarge, got %v", err)
		}
	})

	t.Run("extended payload 64-bit MSB set", func(t *testing.T) {
		// Text frame (0x81), len 127, MSB set (0x80...)
		raw := []byte{0x81, 127, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}
		_, err := ReadFrame(bytes.NewReader(raw))
		if !errors.Is(err, ErrInvalidExtendedPayloadLen) {
			t.Fatalf("expected ErrInvalidExtendedPayloadLen, got %v", err)
		}
	})
}
