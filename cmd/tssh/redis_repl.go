package main

// Built-in Redis RESP protocol client — zero external dependencies
// Implements RESP (REdis Serialization Protocol) v2 for interactive Redis sessions

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// redisRepl starts an interactive Redis REPL over the given TCP connection
func redisRepl(conn net.Conn) {
	reader := bufio.NewReader(conn)
	scanner := bufio.NewScanner(os.Stdin)

	host := conn.RemoteAddr().String()

	// Send PING to verify connection (with 5s timeout)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	sendRedisCommand(conn, []string{"PING"})
	resp, err := readRESP(reader)
	conn.SetReadDeadline(time.Time{}) // Clear timeout
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Redis 连接失败 (PING 超时): %v\n", err)
		fmt.Fprintf(os.Stderr, "   请确认 ECS 跳板与 Redis 在同一 VPC 内\n")
		return
	}
	fmt.Fprintf(os.Stderr, "✅ 已连接 (%s)\n\n", formatRESP(resp))

	for {
		fmt.Printf("%s> ", host)
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.ToLower(line) == "quit" || strings.ToLower(line) == "exit" {
			sendRedisCommand(conn, []string{"QUIT"})
			fmt.Println("OK")
			return
		}

		args := parseRedisArgs(line)
		if len(args) == 0 {
			continue
		}

		sendRedisCommand(conn, args)
		resp, err := readRESP(reader)
		if err != nil {
			if err == io.EOF {
				fmt.Fprintln(os.Stderr, "\n连接已断开")
				return
			}
			fmt.Fprintf(os.Stderr, "❌ 读取响应失败: %v\n", err)
			return
		}
		fmt.Println(formatRESP(resp))
	}
}

// parseRedisArgs splits a command line into arguments (basic quoting support)
func parseRedisArgs(line string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(line); i++ {
		ch := line[i]
		if inQuote {
			if ch == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(ch)
			}
		} else if ch == '"' || ch == '\'' {
			inQuote = true
			quoteChar = ch
		} else if ch == ' ' || ch == '\t' {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

// sendRedisCommand sends a RESP array command
func sendRedisCommand(conn net.Conn, args []string) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*%d\r\n", len(args)))
	for _, arg := range args {
		sb.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(arg), arg))
	}
	conn.Write([]byte(sb.String()))
}

// RESP value types
type respValue struct {
	typ     byte        // '+', '-', ':', '$', '*'
	str     string      // for +, -, $
	integer int64       // for :
	array   []respValue // for *
	isNil   bool        // for null bulk string / null array
}

// readRESP reads one complete RESP value from the reader
func readRESP(r *bufio.Reader) (respValue, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return respValue{}, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return respValue{}, fmt.Errorf("empty response")
	}

	typ := line[0]
	data := line[1:]

	switch typ {
	case '+': // Simple String
		return respValue{typ: '+', str: data}, nil
	case '-': // Error
		return respValue{typ: '-', str: data}, nil
	case ':': // Integer
		n, _ := strconv.ParseInt(data, 10, 64)
		return respValue{typ: ':', integer: n}, nil
	case '$': // Bulk String
		length, _ := strconv.Atoi(data)
		if length == -1 {
			return respValue{typ: '$', isNil: true}, nil
		}
		buf := make([]byte, length+2) // +2 for \r\n
		_, err := io.ReadFull(r, buf)
		if err != nil {
			return respValue{}, err
		}
		return respValue{typ: '$', str: string(buf[:length])}, nil
	case '*': // Array
		count, _ := strconv.Atoi(data)
		if count == -1 {
			return respValue{typ: '*', isNil: true}, nil
		}
		arr := make([]respValue, count)
		for i := 0; i < count; i++ {
			arr[i], err = readRESP(r)
			if err != nil {
				return respValue{}, err
			}
		}
		return respValue{typ: '*', array: arr}, nil
	default:
		return respValue{}, fmt.Errorf("unknown RESP type: %c", typ)
	}
}

// formatRESP formats a RESP value for human-readable display
func formatRESP(v respValue) string {
	switch v.typ {
	case '+':
		return v.str
	case '-':
		return "(error) " + v.str
	case ':':
		return fmt.Sprintf("(integer) %d", v.integer)
	case '$':
		if v.isNil {
			return "(nil)"
		}
		return fmt.Sprintf("\"%s\"", v.str)
	case '*':
		if v.isNil {
			return "(empty array)"
		}
		if len(v.array) == 0 {
			return "(empty array)"
		}
		var sb strings.Builder
		for i, elem := range v.array {
			sb.WriteString(fmt.Sprintf("%d) %s", i+1, formatRESP(elem)))
			if i < len(v.array)-1 {
				sb.WriteString("\n")
			}
		}
		return sb.String()
	default:
		return fmt.Sprintf("(unknown type %c)", v.typ)
	}
}
