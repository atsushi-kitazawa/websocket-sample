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
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"websocket-sample/pkg/websocket"
)

func main() {
	port := flag.Int("port", 8080, "TCP port to listen on")
	pushInterval := flag.Duration("push-interval", 0, "Periodic ticker push interval (e.g. 5s, 0s to disable)")
	stdinPush := flag.Bool("stdin-push", true, "Enable broadcasting messages entered in server stdin")
	chatBroadcast := flag.Bool("chat", true, "Broadcast received client messages to all other clients")
	flag.Parse()

	addr := fmt.Sprintf(":%d", *port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to start TCP listener on %s: %v", addr, err)
	}
	defer func() { _ = listener.Close() }()

	log.Printf("WebSocket server listening on %s", addr)

	// Initialize and run Hub for managing multi-client broadcast
	hub := websocket.NewHub()
	go hub.Run()
	defer hub.Stop()

	// Graceful shutdown handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	var wg sync.WaitGroup
	quitCh := make(chan struct{})

	go func() {
		<-sigCh
		log.Println("Shutting down server...")
		close(quitCh)
		hub.Stop()
		_ = listener.Close()
	}()

	// Trigger 1: Stdin manual broadcast push
	if *stdinPush {
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				msg := fmt.Sprintf("[Server Notice]: %s", line)
				log.Printf("Manual push broadcast: %s", msg)
				hub.BroadcastText(msg)
			}
		}()
		log.Println("Server stdin push enabled (type message and press Enter to broadcast to all clients)")
	}

	// Trigger 2: Periodic ticker automatic push
	if *pushInterval > 0 {
		go func() {
			ticker := time.NewTicker(*pushInterval)
			defer ticker.Stop()
			for {
				select {
				case t := <-ticker.C:
					msg := fmt.Sprintf("[Server Ticker]: %s | active_clients: %d", t.Format("15:04:05"), hub.ClientCount())
					log.Printf("Ticker push: %s", msg)
					hub.BroadcastText(msg)
				case <-quitCh:
					return
				}
			}
		}()
		log.Printf("Periodic push enabled with interval: %v", *pushInterval)
	}

	// Connection accept loop
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-quitCh:
				log.Println("Listener closed. Waiting for active connections to finish...")
				wg.Wait()
				log.Println("Server stopped cleanly.")
				return
			default:
				log.Printf("Error accepting connection: %v", err)
				continue
			}
		}

		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			handleClient(c, hub, *chatBroadcast)
		}(conn)
	}
}

func handleClient(conn net.Conn, hub *websocket.Hub, chatBroadcast bool) {
	remoteAddr := conn.RemoteAddr().String()
	log.Printf("[%s] New connection accepted", remoteAddr)
	defer log.Printf("[%s] Connection closed", remoteAddr)

	// 1. Perform WebSocket Handshake
	br, err := websocket.ServerHandshake(conn, conn)
	if err != nil {
		log.Printf("[%s] Handshake failed: %v", remoteAddr, err)
		_ = conn.Close()
		return
	}
	log.Printf("[%s] WebSocket handshake completed successfully", remoteAddr)

	// 2. Register client with Hub and start dedicated WritePump
	client := websocket.NewClient(conn, hub, remoteAddr, websocket.DefaultSendBufferSize)
	hub.Register(client)
	defer client.Close()

	// WritePump handles all sequential writing to conn from client.Send channel
	go client.WritePump()

	// Notify other clients about join
	if chatBroadcast {
		hub.BroadcastText(fmt.Sprintf("[System]: Client %s joined (total: %d)", remoteAddr, hub.ClientCount()))
	}

	// 3. Frame processing read loop
	for {
		frame, err := websocket.ReadFrame(br)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
				log.Printf("[%s] Client disconnected", remoteAddr)
			} else {
				log.Printf("[%s] Error reading frame: %v", remoteAddr, err)
			}
			if chatBroadcast {
				hub.BroadcastText(fmt.Sprintf("[System]: Client %s left", remoteAddr))
			}
			return
		}

		// RFC 6455 Section 5.1: Client-to-server frames MUST be masked.
		if !frame.Masked {
			log.Printf("[%s] Protocol error: unmasked frame received from client", remoteAddr)
			closeFrame := websocket.NewCloseFrame(websocket.CloseProtocolError, "unmasked client frame", false)
			select {
			case client.Send <- closeFrame:
			default:
			}
			return
		}

		switch frame.Opcode {
		case websocket.OpcodeText:
			msg := string(frame.Payload)
			log.Printf("[%s] Received Text: %s", remoteAddr, msg)

			if chatBroadcast {
				// Broadcast to all clients
				hub.BroadcastText(fmt.Sprintf("[%s]: %s", remoteAddr, msg))
			} else {
				// Echo only to sender
				echoMsg := fmt.Sprintf("echo: %s", msg)
				select {
				case client.Send <- websocket.NewTextFrame([]byte(echoMsg), false):
				default:
				}
			}

		case websocket.OpcodeBinary:
			log.Printf("[%s] Received Binary (%d bytes)", remoteAddr, len(frame.Payload))
			select {
			case client.Send <- websocket.NewBinaryFrame(frame.Payload, false):
			default:
			}

		case websocket.OpcodePing:
			log.Printf("[%s] Received Ping, sending Pong", remoteAddr)
			select {
			case client.Send <- websocket.NewPongFrame(frame.Payload, false):
			default:
			}

		case websocket.OpcodePong:
			log.Printf("[%s] Received Pong heartbeat", remoteAddr)

		case websocket.OpcodeClose:
			code, reason, _ := frame.ParseClosePayload()
			log.Printf("[%s] Received Close frame (code: %d, reason: %q)", remoteAddr, code, reason)

			// Reply with Close frame acknowledgement
			closeResp := websocket.NewCloseFrame(websocket.CloseNormalClosure, "", false)
			select {
			case client.Send <- closeResp:
			default:
			}
			return

		default:
			log.Printf("[%s] Unsupported opcode: 0x%X", remoteAddr, frame.Opcode)
			closeFrame := websocket.NewCloseFrame(websocket.CloseProtocolError, "unsupported opcode", false)
			select {
			case client.Send <- closeFrame:
			default:
			}
			return
		}
	}
}
