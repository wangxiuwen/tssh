package main

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

// AxtMessage types (from official cloud-assistant-starter AxtMessage.ts)
const (
	MsgInput  uint32 = 0 // Client→Server
	MsgOutput uint32 = 1 // Server→Client
	MsgResize uint32 = 2 // Client→Server
	MsgClose  uint32 = 3 // Client→Server
	MsgOpen   uint32 = 4 // Client→Server
	MsgStatus uint32 = 5 // Server→Client
	MsgSync   uint32 = 6 // Bidirectional
)

// AxtMessage represents a Cloud Assistant session WebSocket message
type AxtMessage struct {
	MsgType    uint32
	Version    string // 4 bytes
	ChannelID  string // variable
	InstanceID string // variable
	Timestamp  uint64
	InputSeq   uint32
	OutputSeq  uint32
	MsgLength  uint16
	Encoding   uint8
	Reserved   uint8
	Payload    []byte
}

func decodeAxtMessage(data []byte) (*AxtMessage, error) {
	if len(data) < 30 {
		return nil, fmt.Errorf("too short: %d", len(data))
	}
	msg := &AxtMessage{}
	off := 0

	msg.MsgType = binary.LittleEndian.Uint32(data[off:])
	off += 4

	msg.Version = string(data[off : off+4])
	off += 4

	l1 := int(data[off])
	off++
	if l1 > 0 && off+l1 <= len(data) {
		msg.ChannelID = string(data[off : off+l1])
		off += l1
	}

	l2 := int(data[off])
	off++
	if l2 > 0 && off+l2 <= len(data) {
		msg.InstanceID = string(data[off : off+l2])
		off += l2
	}

	if off+20 > len(data) {
		return nil, fmt.Errorf("truncated at offset %d", off)
	}

	msg.Timestamp = binary.LittleEndian.Uint64(data[off:])
	off += 8
	msg.InputSeq = binary.LittleEndian.Uint32(data[off:])
	off += 4
	msg.OutputSeq = binary.LittleEndian.Uint32(data[off:])
	off += 4
	msg.MsgLength = binary.LittleEndian.Uint16(data[off:])
	off += 2
	msg.Encoding = data[off]
	off++
	msg.Reserved = data[off]
	off++

	if off < len(data) {
		msg.Payload = data[off:]
	}
	return msg, nil
}

func encodeAxtMessage(msg *AxtMessage) []byte {
	l1 := len(msg.ChannelID)
	l2 := len(msg.InstanceID)
	// header: 4+4+1+l1+1+l2+8+4+4+2+1+1 = 30 + l1 + l2
	total := 30 + l1 + l2 + len(msg.Payload)
	buf := make([]byte, total)
	off := 0

	binary.LittleEndian.PutUint32(buf[off:], msg.MsgType)
	off += 4

	// Version: exactly 4 bytes, pad with nul
	ver := []byte(msg.Version)
	for i := 0; i < 4; i++ {
		if i < len(ver) {
			buf[off+i] = ver[i]
		}
	}
	off += 4

	buf[off] = byte(l1)
	off++
	copy(buf[off:], msg.ChannelID)
	off += l1

	buf[off] = byte(l2)
	off++
	copy(buf[off:], msg.InstanceID)
	off += l2

	binary.LittleEndian.PutUint64(buf[off:], msg.Timestamp)
	off += 8
	binary.LittleEndian.PutUint32(buf[off:], msg.InputSeq)
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], msg.OutputSeq)
	off += 4
	binary.LittleEndian.PutUint16(buf[off:], msg.MsgLength)
	off += 2
	buf[off] = msg.Encoding
	off++
	buf[off] = msg.Reserved
	off++
	copy(buf[off:], msg.Payload)
	return buf
}

// ConnectSession establishes an interactive terminal via StartTerminalSession + WebSocket
func ConnectSession(config *Config, instanceID string) error {
	client, err := NewAliyunClient(config)
	if err != nil {
		return err
	}

	wsURL, _, _, err := client.StartSession(instanceID)
	if err != nil {
		return err
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket failed: %w", err)
	}
	defer conn.Close()

	// Raw terminal mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("raw mode failed: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	channelID := ""
	var inputSeq uint32
	var outputSeq uint32

	mkMsg := func(t uint32, payload []byte) []byte {
		msg := &AxtMessage{
			MsgType:   t,
			Version:   "1.02",
			ChannelID: channelID,
			// instanceId is empty in client-sent messages (per official code)
			InstanceID: "",
			Timestamp:  uint64(time.Now().UnixMilli()),
			InputSeq:   inputSeq,
			OutputSeq:  outputSeq,
			MsgLength:  uint16(len(payload)),
			Payload:    payload,
		}
		return encodeAxtMessage(msg)
	}

	sendResize := func() {
		cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			return
		}
		// Per official code: rows first, then cols (both int16 LE)
		p := make([]byte, 4)
		binary.LittleEndian.PutUint16(p[0:], uint16(rows))
		binary.LittleEndian.PutUint16(p[2:], uint16(cols))
		inputSeq++
		conn.WriteMessage(websocket.BinaryMessage, mkMsg(MsgResize, p))
	}

	// Resize handler
	sigCh := make(chan os.Signal, 1)
	notifyResize(sigCh)
	go func() {
		for range sigCh {
			sendResize()
		}
	}()

	// Heartbeat every 50s
	go func() {
		ticker := time.NewTicker(50 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			conn.WriteMessage(websocket.BinaryMessage, mkMsg(MsgInput, nil))
		}
	}()

	done := make(chan struct{})
	firstOutput := true

	// WebSocket → stdout
	go func() {
		defer close(done)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			msg, err := decodeAxtMessage(data)
			if err != nil {
				continue
			}

			// Learn channelId from first message
			if channelID == "" && msg.ChannelID != "" {
				channelID = msg.ChannelID
			}

			switch msg.MsgType {
			case MsgOutput:
				outputSeq = msg.OutputSeq
				if msg.Payload != nil {
					os.Stdout.Write(msg.Payload)
				}
				// Send resize after first output
				if firstOutput {
					firstOutput = false
					sendResize()
				}

			case MsgStatus:
				if len(msg.Payload) > 0 {
					state := msg.Payload[0]
					// Closed(5) or Exited(6) → disconnect
					if state == 5 || state == 6 {
						return
					}
				}

			case MsgSync:
				// Respond with sync
				conn.WriteMessage(websocket.BinaryMessage, mkMsg(MsgSync, nil))
			}
		}
	}()

	// stdin → WebSocket
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			inputSeq++
			conn.WriteMessage(websocket.BinaryMessage, mkMsg(MsgInput, buf[:n]))
		}
	}()

	<-done
	return nil
}

// PortForward via StartTerminalSession with PortNumber
func PortForward(config *Config, instanceID string, localPort, remotePort int) error {
	client, err := NewAliyunClient(config)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}
	defer listener.Close()

	fmt.Printf("📡 端口转发: 127.0.0.1:%d → remote:%d\n", localPort, remotePort)
	fmt.Println("Press Ctrl+C to stop")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		listener.Close()
	}()

	for {
		localConn, err := listener.Accept()
		if err != nil {
			return nil
		}
		go func() {
			defer localConn.Close()
			wsURL, _, _, err := client.StartPortForwardSession(instanceID, remotePort)
			if err != nil {
				return
			}
			wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				return
			}
			defer wsConn.Close()

			ch := make(chan struct{}, 2)
			go func() {
				buf := make([]byte, 32768)
				for {
					n, err := localConn.Read(buf)
					if err != nil {
						break
					}
					wsConn.WriteMessage(websocket.BinaryMessage, buf[:n])
				}
				ch <- struct{}{}
			}()
			go func() {
				for {
					_, msg, err := wsConn.ReadMessage()
					if err != nil {
						break
					}
					localConn.Write(msg)
				}
				ch <- struct{}{}
			}()
			<-ch
		}()
	}
}

// Helpers

func sleepDuration(seconds int) {
	time.Sleep(time.Duration(seconds) * time.Second)
}

func sleepMs(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func readFileBase64(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func findFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 54321
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func execCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// Suppress unused import warnings
var _ = io.ReadAll
