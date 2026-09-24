package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"websocket-sample/pkg/websocket"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "Server TCP address to connect to")
	path := flag.String("path", "/", "WebSocket URL path")
	msg := flag.String("msg", "", "Single message to send and wait for echo (non-interactive mode)")
	flag.Parse()

	// 1. Establish TCP connection
	log.Printf("Connecting to %s...", *addr)
	conn, err := net.DialTimeout("tcp", *addr, 5*time.Second)
	if err != nil {
		log.Fatalf("Failed to connect to %s: %v", *addr, err)
	}
	defer func() { _ = conn.Close() }()

	log.Printf("TCP connection established. Sending WebSocket handshake...")

	// 2. Perform WebSocket Handshake
	br, err := websocket.ClientHandshake(conn, conn, *addr, *path)
	if err != nil {
		log.Fatalf("WebSocket handshake failed: %v", err)
	}
	log.Println("WebSocket handshake succeeded! Connection upgraded.")

	// Channel to signal client completion
	doneCh := make(chan struct{})

	// 3. Start server frame reader in background
	go func() {
		defer close(doneCh)
		for {
			frame, err := websocket.ReadFrame(br)
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
					log.Println("Server closed connection.")
				} else {
					log.Printf("Error reading from server: %v", err)
				}
				return
			}

			switch frame.Opcode {
			case websocket.OpcodeText:
				fmt.Printf("\n[Server]: %s\n> ", string(frame.Payload))

			case websocket.OpcodeBinary:
				fmt.Printf("\n[Server Binary]: %d bytes\n> ", len(frame.Payload))

			case websocket.OpcodePing:
				log.Println("\n[Server Ping]: received Ping, sending Pong...")
				pong := websocket.NewPongFrame(frame.Payload, true)
				_ = websocket.WriteFrame(conn, pong)
				fmt.Print("> ")

			case websocket.OpcodePong:
				fmt.Println("\n[Server Pong]: heartbeat response received")
				fmt.Print("> ")

			case websocket.OpcodeClose:
				code, reason, _ := frame.ParseClosePayload()
				log.Printf("\n[Server Close]: code=%d reason=%q", code, reason)
				// Send Close ack if server initiated
				ack := websocket.NewCloseFrame(websocket.CloseNormalClosure, "", true)
				_ = websocket.WriteFrame(conn, ack)
				return

			default:
				log.Printf("\n[Server Unknown Opcode]: 0x%X\n> ", frame.Opcode)
			}
		}
	}()

	// 4. Send message or start interactive prompt
	if *msg != "" {
		// Single message mode
		log.Printf("Sending message: %q", *msg)
		frame := websocket.NewTextFrame([]byte(*msg), true)
		if err := websocket.WriteFrame(conn, frame); err != nil {
			log.Fatalf("Failed to write frame: %v", err)
		}

		// Wait briefly for echo or server close
		select {
		case <-doneCh:
		case <-time.After(2 * time.Second):
			// Send graceful close
			closeFrame := websocket.NewCloseFrame(websocket.CloseNormalClosure, "done", true)
			_ = websocket.WriteFrame(conn, closeFrame)
			select {
			case <-doneCh:
			case <-time.After(1 * time.Second):
			}
		}
		log.Println("Done.")
		return
	}

	// Interactive Mode
	fmt.Println("-------------------------------------------------------------")
	fmt.Println("Type message and press Enter to send.")
	fmt.Println("Commands:")
	fmt.Println("  /ping       - Send a Ping frame")
	fmt.Println("  /close      - Send a Close frame and exit")
	fmt.Println("  /quit, /exit- Exit immediately")
	fmt.Println("-------------------------------------------------------------")
	fmt.Print("> ")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Print("> ")
			continue
		}

		switch line {
		case "/quit", "/exit":
			log.Println("Exiting...")
			return

		case "/ping":
			log.Println("Sending Ping frame...")
			ping := websocket.NewPingFrame([]byte("ping-payload"), true)
			if err := websocket.WriteFrame(conn, ping); err != nil {
				log.Printf("Failed to send Ping: %v", err)
				return
			}

		case "/close":
			log.Println("Sending Close frame...")
			closeFrame := websocket.NewCloseFrame(websocket.CloseNormalClosure, "client exit", true)
			if err := websocket.WriteFrame(conn, closeFrame); err != nil {
				log.Printf("Failed to send Close: %v", err)
			}
			select {
			case <-doneCh:
			case <-time.After(2 * time.Second):
			}
			return

		default:
			// Send text frame (masked)
			frame := websocket.NewTextFrame([]byte(line), true)
			if err := websocket.WriteFrame(conn, frame); err != nil {
				log.Printf("Failed to write frame: %v", err)
				return
			}
		}

		// Wait a little before showing next prompt to give echo a chance to print
		time.Sleep(30 * time.Millisecond)
		fmt.Print("> ")
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		log.Printf("Error reading stdin: %v", err)
	}
}
