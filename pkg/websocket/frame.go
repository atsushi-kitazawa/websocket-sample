package websocket

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Opcode represents the WebSocket frame opcode as defined in RFC 6455 Section 5.2.
type Opcode byte

const (
	OpcodeContinuation Opcode = 0x0
	OpcodeText         Opcode = 0x1
	OpcodeBinary       Opcode = 0x2
	OpcodeClose        Opcode = 0x8
	OpcodePing         Opcode = 0x9
	OpcodePong         Opcode = 0xA
)

// IsControl returns true if the opcode represents a control frame (Close, Ping, Pong).
func (op Opcode) IsControl() bool {
	return op >= 0x8
}

// IsValid checks if the opcode is a valid RFC 6455 opcode.
func (op Opcode) IsValid() bool {
	switch op {
	case OpcodeContinuation, OpcodeText, OpcodeBinary, OpcodeClose, OpcodePing, OpcodePong:
		return true
	default:
		return false
	}
}

// Close status codes defined in RFC 6455 Section 7.4.1.
const (
	CloseNormalClosure           uint16 = 1000
	CloseGoingAway               uint16 = 1001
	CloseProtocolError           uint16 = 1002
	CloseUnsupportedData         uint16 = 1003
	CloseNoStatusReceived        uint16 = 1005
	CloseAbnormalClosure         uint16 = 1006
	CloseInvalidFramePayloadData uint16 = 1007
	ClosePolicyViolation         uint16 = 1008
	CloseMessageTooBig           uint16 = 1009
	CloseMandatoryExtension      uint16 = 1010
	CloseInternalServerError     uint16 = 1011
)

// MaxPayloadLength defines the safety limit (32MB) for single frame payload.
const MaxPayloadLength uint64 = 32 * 1024 * 1024

var (
	ErrReservedBitsNonZero       = errors.New("websocket: RSV1/RSV2/RSV3 bits must be 0")
	ErrInvalidOpcode             = errors.New("websocket: invalid opcode")
	ErrControlFrameFragmented    = errors.New("websocket: control frames must not be fragmented (FIN must be true)")
	ErrControlFrameTooLarge      = errors.New("websocket: control frame payload length must be <= 125 bytes")
	ErrPayloadTooLarge           = errors.New("websocket: payload length exceeds maximum allowed limit")
	ErrInvalidExtendedPayloadLen = errors.New("websocket: extended payload length most significant bit must be 0")
	ErrInvalidClosePayload       = errors.New("websocket: close frame payload cannot be 1 byte")
)

// Frame represents a single WebSocket frame.
type Frame struct {
	Fin        bool
	Rsv1       bool
	Rsv2       bool
	Rsv3       bool
	Opcode     Opcode
	Masked     bool
	MaskingKey [4]byte
	Payload    []byte
}

// NewTextFrame creates an unfragmented Text frame.
func NewTextFrame(payload []byte, masked bool) *Frame {
	return &Frame{
		Fin:     true,
		Opcode:  OpcodeText,
		Masked:  masked,
		Payload: payload,
	}
}

// NewBinaryFrame creates an unfragmented Binary frame.
func NewBinaryFrame(payload []byte, masked bool) *Frame {
	return &Frame{
		Fin:     true,
		Opcode:  OpcodeBinary,
		Masked:  masked,
		Payload: payload,
	}
}

// NewPingFrame creates a Ping control frame.
func NewPingFrame(payload []byte, masked bool) *Frame {
	return &Frame{
		Fin:     true,
		Opcode:  OpcodePing,
		Masked:  masked,
		Payload: payload,
	}
}

// NewPongFrame creates a Pong control frame.
func NewPongFrame(payload []byte, masked bool) *Frame {
	return &Frame{
		Fin:     true,
		Opcode:  OpcodePong,
		Masked:  masked,
		Payload: payload,
	}
}

// NewCloseFrame creates a Close control frame with status code and optional reason.
func NewCloseFrame(statusCode uint16, reason string, masked bool) *Frame {
	var payload []byte
	if statusCode != 0 {
		payload = make([]byte, 2+len(reason))
		binary.BigEndian.PutUint16(payload[:2], statusCode)
		copy(payload[2:], reason)
	}
	return &Frame{
		Fin:     true,
		Opcode:  OpcodeClose,
		Masked:  masked,
		Payload: payload,
	}
}

// ParseClosePayload extracts the status code and reason string from a Close frame's payload.
func (f *Frame) ParseClosePayload() (uint16, string, error) {
	if len(f.Payload) == 0 {
		return 0, "", nil
	}
	if len(f.Payload) == 1 {
		return 0, "", ErrInvalidClosePayload
	}
	code := binary.BigEndian.Uint16(f.Payload[:2])
	reason := string(f.Payload[2:])
	return code, reason, nil
}

// MaskPayload applies the 4-byte XOR mask in-place to the payload data.
// Because XOR is symmetric, calling MaskPayload again with the same key unmasks the data.
func MaskPayload(payload []byte, key [4]byte) {
	for i := range payload {
		payload[i] ^= key[i%4]
	}
}

// ReadFrame reads and decodes a single WebSocket frame from r according to RFC 6455 Section 5.2.
func ReadFrame(r io.Reader) (*Frame, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}

	b0 := header[0]
	b1 := header[1]

	frame := &Frame{
		Fin:    (b0 & 0x80) != 0,
		Rsv1:   (b0 & 0x40) != 0,
		Rsv2:   (b0 & 0x20) != 0,
		Rsv3:   (b0 & 0x10) != 0,
		Opcode: Opcode(b0 & 0x0F),
		Masked: (b1 & 0x80) != 0,
	}

	// RSV bits MUST be 0 unless an extension is negotiated.
	if frame.Rsv1 || frame.Rsv2 || frame.Rsv3 {
		return nil, ErrReservedBitsNonZero
	}

	// Opcode validation
	if !frame.Opcode.IsValid() {
		return nil, fmt.Errorf("%w: 0x%X", ErrInvalidOpcode, frame.Opcode)
	}

	// Payload length
	payloadLen7 := b1 & 0x7F
	var payloadLength uint64

	switch payloadLen7 {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return nil, fmt.Errorf("read extended 16-bit payload length: %w", err)
		}
		payloadLength = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return nil, fmt.Errorf("read extended 64-bit payload length: %w", err)
		}
		payloadLength = binary.BigEndian.Uint64(ext[:])
		// The most significant bit MUST be 0.
		if (payloadLength & 0x8000000000000000) != 0 {
			return nil, ErrInvalidExtendedPayloadLen
		}
	default:
		payloadLength = uint64(payloadLen7)
	}

	// Control frame checks (RFC 6455 Section 5.5)
	if frame.Opcode.IsControl() {
		if !frame.Fin {
			return nil, ErrControlFrameFragmented
		}
		if payloadLength > 125 {
			return nil, ErrControlFrameTooLarge
		}
	}

	if payloadLength > MaxPayloadLength {
		return nil, ErrPayloadTooLarge
	}

	// Read masking key if masked
	if frame.Masked {
		if _, err := io.ReadFull(r, frame.MaskingKey[:]); err != nil {
			return nil, fmt.Errorf("read masking key: %w", err)
		}
	}

	// Read payload
	if payloadLength > 0 {
		frame.Payload = make([]byte, payloadLength)
		if _, err := io.ReadFull(r, frame.Payload); err != nil {
			return nil, fmt.Errorf("read payload: %w", err)
		}
		if frame.Masked {
			MaskPayload(frame.Payload, frame.MaskingKey)
		}
	}

	// Validate close frame payload if present
	if frame.Opcode == OpcodeClose && len(frame.Payload) == 1 {
		return nil, ErrInvalidClosePayload
	}

	return frame, nil
}

// WriteFrame encodes and writes a single WebSocket frame to w.
func WriteFrame(w io.Writer, frame *Frame) error {
	b0 := byte(frame.Opcode & 0x0F)
	if frame.Fin {
		b0 |= 0x80
	}
	if frame.Rsv1 {
		b0 |= 0x40
	}
	if frame.Rsv2 {
		b0 |= 0x20
	}
	if frame.Rsv3 {
		b0 |= 0x10
	}

	payloadLen := len(frame.Payload)
	var headerBuf []byte

	var lenByte byte
	if payloadLen <= 125 {
		lenByte = byte(payloadLen)
		if frame.Masked {
			lenByte |= 0x80
		}
		headerBuf = append(headerBuf, b0, lenByte)
	} else if payloadLen <= 65535 {
		lenByte = 126
		if frame.Masked {
			lenByte |= 0x80
		}
		headerBuf = append(headerBuf, b0, lenByte, byte(payloadLen>>8), byte(payloadLen))
	} else {
		lenByte = 127
		if frame.Masked {
			lenByte |= 0x80
		}
		extLen := make([]byte, 8)
		binary.BigEndian.PutUint64(extLen, uint64(payloadLen))
		headerBuf = append(headerBuf, b0, lenByte)
		headerBuf = append(headerBuf, extLen...)
	}

	maskKey := frame.MaskingKey
	if frame.Masked {
		// If no masking key is provided, generate a random one
		if maskKey == [4]byte{0, 0, 0, 0} {
			if _, err := io.ReadFull(rand.Reader, maskKey[:]); err != nil {
				return fmt.Errorf("generate masking key: %w", err)
			}
		}
		headerBuf = append(headerBuf, maskKey[:]...)
	}

	// Write header
	if _, err := w.Write(headerBuf); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}

	// Write payload
	if payloadLen > 0 {
		if frame.Masked {
			maskedPayload := make([]byte, payloadLen)
			copy(maskedPayload, frame.Payload)
			MaskPayload(maskedPayload, maskKey)
			if _, err := w.Write(maskedPayload); err != nil {
				return fmt.Errorf("write masked payload: %w", err)
			}
		} else {
			if _, err := w.Write(frame.Payload); err != nil {
				return fmt.Errorf("write payload: %w", err)
			}
		}
	}

	return nil
}
