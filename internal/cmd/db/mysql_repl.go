package db

// Built-in MySQL wire protocol client — zero external dependencies
// Implements MySQL Client/Server protocol for basic interactive SQL sessions
// Reference: https://dev.mysql.com/doc/dev/mysql-server/latest/page_protocol_basics.html

import (
	"bufio"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strings"
	"text/tabwriter"
)

// MySQL packet constants
const (
	mysqlComQuery  = 0x03
	mysqlComQuit   = 0x01
	mysqlOKPacket  = 0x00
	mysqlErrPacket = 0xff
	mysqlEOFPacket = 0xfe
)

// mysqlConn wraps a MySQL TCP connection
type mysqlConn struct {
	conn   net.Conn
	reader *bufio.Reader
	seq    byte
}

// readPacket reads a single MySQL packet
func (c *mysqlConn) readPacket() ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(c.reader, header); err != nil {
		return nil, err
	}
	length := int(uint32(header[0]) | uint32(header[1])<<8 | uint32(header[2])<<16)
	c.seq = header[3] + 1

	payload := make([]byte, length)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// writePacket sends a MySQL packet
func (c *mysqlConn) writePacket(payload []byte) error {
	length := len(payload)
	header := []byte{byte(length), byte(length >> 8), byte(length >> 16), c.seq}
	c.seq++
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(payload)
	return err
}

// mysqlHandshake performs the MySQL initial handshake
func mysqlHandshake(mc *mysqlConn, user, password string) error {
	// Read server greeting
	data, err := mc.readPacket()
	if err != nil {
		return fmt.Errorf("read greeting: %w", err)
	}
	// Defensive: a truncated or empty greeting would otherwise panic on data[0]
	// when the tunnel dies mid-handshake or the server returns junk.
	if len(data) == 0 {
		return fmt.Errorf("greeting 为空, 远端可能非 MySQL 或连接中断")
	}
	if data[0] == mysqlErrPacket {
		msg := ""
		if len(data) > 9 {
			msg = string(data[9:])
		}
		return fmt.Errorf("server error: %s", msg)
	}

	// Parse greeting (protocol 10)
	pos := 1
	// Server version (null-terminated)
	nullPos := pos
	for nullPos < len(data) && data[nullPos] != 0 {
		nullPos++
	}
	serverVersion := string(data[pos:nullPos])
	pos = nullPos + 1

	// Connection ID (4 bytes) + auth-plugin-data part 1 (8 bytes): need 12 bytes ahead.
	if pos+12 > len(data) {
		return fmt.Errorf("greeting 过短 (%d 字节), 无法解析 connection id + auth data", len(data))
	}
	pos += 4

	// Auth-plugin-data part 1 (8 bytes)
	authData := make([]byte, 20)
	copy(authData[:8], data[pos:pos+8])
	pos += 8

	// Filler (1 byte)
	pos++

	// Capability flags lower 2 bytes
	pos += 2

	// Character set, status flags, capability flags upper 2 bytes
	if pos < len(data) {
		pos++    // charset
		pos += 2 // status
		pos += 2 // cap upper
	}

	// Length of auth-plugin-data (1 byte)
	authDataLen := 0
	if pos < len(data) {
		authDataLen = int(data[pos])
		pos++
	}

	// Reserved (10 bytes)
	pos += 10

	// Auth-plugin-data part 2
	if pos < len(data) {
		part2Len := authDataLen - 8
		if part2Len > 12 {
			part2Len = 12
		}
		if part2Len < 0 {
			part2Len = 12
		}
		if pos+part2Len <= len(data) {
			copy(authData[8:], data[pos:pos+part2Len])
		}
	}

	_ = serverVersion

	// Build auth response (mysql_native_password)
	var authResp []byte
	if password != "" {
		authResp = scramblePassword(authData[:20], []byte(password))
	}

	// Client capabilities
	clientCap := uint32(0x0000_a685) // PROTOCOL_41 | SECURE_CONNECTION | PLUGIN_AUTH | LONG_FLAG etc
	// Auth response
	resp := make([]byte, 0, 128)
	// Capability flags (4 bytes)
	resp = append(resp, byte(clientCap), byte(clientCap>>8), byte(clientCap>>16), byte(clientCap>>24))
	// Max packet size (4 bytes)
	maxPacket := uint32(16*1024*1024 - 1)
	resp = append(resp, byte(maxPacket), byte(maxPacket>>8), byte(maxPacket>>16), byte(maxPacket>>24))
	// Character set (utf8mb4 = 45)
	resp = append(resp, 45)
	// Reserved 23 bytes
	resp = append(resp, make([]byte, 23)...)
	// Username (null-terminated)
	resp = append(resp, []byte(user)...)
	resp = append(resp, 0)
	// Auth response length + data
	resp = append(resp, byte(len(authResp)))
	resp = append(resp, authResp...)
	// Database (none) — skip
	// Plugin name
	resp = append(resp, []byte("mysql_native_password")...)
	resp = append(resp, 0)

	mc.seq = 1
	if err := mc.writePacket(resp); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}

	// Read auth result
	data, err = mc.readPacket()
	if err != nil {
		return fmt.Errorf("read auth result: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("auth result 为空, 连接可能中断")
	}
	if data[0] == mysqlErrPacket {
		_, msg := parseMysqlError(data)
		return fmt.Errorf("认证失败: %s", msg)
	}
	return nil
}

// scramblePassword implements mysql_native_password auth
func scramblePassword(scramble, password []byte) []byte {
	// SHA1(password) XOR SHA1(scramble + SHA1(SHA1(password)))
	hash1 := sha1.Sum(password)
	hash2 := sha1.Sum(hash1[:])

	h := sha1.New()
	h.Write(scramble)
	h.Write(hash2[:])
	hash3 := h.Sum(nil)

	result := make([]byte, 20)
	for i := range result {
		result[i] = hash1[i] ^ hash3[i]
	}
	return result
}

// parseMysqlError extracts error code and message
func parseMysqlError(data []byte) (int, string) {
	if len(data) < 3 {
		return 0, "unknown error"
	}
	code := int(binary.LittleEndian.Uint16(data[1:3]))
	msg := ""
	if len(data) > 9 {
		msg = string(data[9:])
	} else if len(data) > 3 {
		msg = string(data[3:])
	}
	return code, msg
}

// mysqlQuery sends a COM_QUERY and prints results
func mysqlQuery(mc *mysqlConn, query string) error {
	mc.seq = 0
	payload := append([]byte{mysqlComQuery}, []byte(query)...)
	if err := mc.writePacket(payload); err != nil {
		return err
	}

	// Read result
	data, err := mc.readPacket()
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("query response 为空, 连接可能中断")
	}

	switch data[0] {
	case mysqlOKPacket:
		affectedRows, pos := readLenEnc(data[1:])
		lastInsertID, _ := readLenEnc(data[1+pos:])
		fmt.Printf("Query OK, %d rows affected (last insert id: %d)\n", affectedRows, lastInsertID)
		return nil
	case mysqlErrPacket:
		_, msg := parseMysqlError(data)
		fmt.Printf("ERROR: %s\n", msg)
		return nil
	case mysqlEOFPacket:
		fmt.Println("Query OK")
		return nil
	}

	// Result set: first packet is column count
	colCount, _ := readLenEnc(data)

	// Read column definitions
	colNames := make([]string, colCount)
	for i := 0; i < int(colCount); i++ {
		colData, err := mc.readPacket()
		if err != nil {
			return err
		}
		colNames[i] = parseColumnName(colData)
	}

	// EOF after columns
	eof, err := mc.readPacket()
	if err != nil {
		return err
	}
	if len(eof) > 0 && eof[0] != mysqlEOFPacket {
		// Might be deprecated EOF
	}

	// Read rows
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	// Header
	fmt.Fprintln(w, strings.Join(colNames, "\t"))
	rowCount := 0
	for {
		rowData, err := mc.readPacket()
		if err != nil {
			break
		}
		if len(rowData) > 0 && rowData[0] == mysqlEOFPacket && len(rowData) < 9 {
			break
		}
		if len(rowData) > 0 && rowData[0] == mysqlErrPacket {
			_, msg := parseMysqlError(rowData)
			fmt.Printf("ERROR: %s\n", msg)
			break
		}
		// Parse row values
		values := make([]string, colCount)
		pos := 0
		for i := 0; i < int(colCount); i++ {
			if pos >= len(rowData) {
				break
			}
			if rowData[pos] == 0xfb {
				values[i] = "NULL"
				pos++
			} else {
				strLen, n := readLenEnc(rowData[pos:])
				pos += n
				if pos+int(strLen) <= len(rowData) {
					values[i] = string(rowData[pos : pos+int(strLen)])
				}
				pos += int(strLen)
			}
		}
		fmt.Fprintln(w, strings.Join(values, "\t"))
		rowCount++
	}
	w.Flush()
	fmt.Printf("\n%d rows in set\n", rowCount)
	return nil
}

// readLenEnc reads a length-encoded integer and returns value + bytes consumed
func readLenEnc(data []byte) (uint64, int) {
	if len(data) == 0 {
		return 0, 0
	}
	switch {
	case data[0] < 0xfb:
		return uint64(data[0]), 1
	case data[0] == 0xfc:
		if len(data) < 3 {
			return 0, 1
		}
		return uint64(binary.LittleEndian.Uint16(data[1:3])), 3
	case data[0] == 0xfd:
		if len(data) < 4 {
			return 0, 1
		}
		return uint64(data[1]) | uint64(data[2])<<8 | uint64(data[3])<<16, 4
	case data[0] == 0xfe:
		if len(data) < 9 {
			return 0, 1
		}
		return binary.LittleEndian.Uint64(data[1:9]), 9
	default:
		return math.MaxUint64, 1 // NULL
	}
}

// parseColumnName extracts the column name from a column definition packet
func parseColumnName(data []byte) string {
	pos := 0
	// Skip: catalog, schema, table, org_table (4 length-encoded strings)
	for i := 0; i < 4; i++ {
		if pos >= len(data) {
			return "?"
		}
		strLen, n := readLenEnc(data[pos:])
		pos += n + int(strLen)
	}
	// 5th is the column name
	if pos >= len(data) {
		return "?"
	}
	strLen, n := readLenEnc(data[pos:])
	pos += n
	if pos+int(strLen) <= len(data) {
		return string(data[pos : pos+int(strLen)])
	}
	return "?"
}

// mysqlRepl starts an interactive MySQL REPL
func mysqlRepl(conn net.Conn, user, password string) {
	mc := &mysqlConn{conn: conn, reader: bufio.NewReader(conn)}

	if err := mysqlHandshake(mc, user, password); err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		return
	}

	host := conn.RemoteAddr().String()
	fmt.Fprintf(os.Stderr, "✅ 已连接到 MySQL %s (用户: %s)\n", host, user)
	fmt.Fprintf(os.Stderr, "输入 SQL 命令 (quit 退出):\n\n")

	scanner := bufio.NewScanner(os.Stdin)
	// Raise buffer: default 64KB trips on any non-trivial paste (large INSERT,
	// multi-line CREATE TABLE). 16MB matches MySQL's default max_allowed_packet.
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var multiLine strings.Builder

	for {
		if multiLine.Len() == 0 {
			fmt.Printf("mysql> ")
		} else {
			fmt.Printf("    -> ")
		}

		if !scanner.Scan() {
			break
		}
		line := scanner.Text()

		if multiLine.Len() == 0 {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if strings.ToLower(trimmed) == "quit" || strings.ToLower(trimmed) == "exit" {
				mc.seq = 0
				mc.writePacket([]byte{mysqlComQuit})
				fmt.Println("Bye")
				return
			}
		}

		if multiLine.Len() > 0 {
			multiLine.WriteString("\n")
		}
		multiLine.WriteString(line)

		query := strings.TrimSpace(multiLine.String())
		if !strings.HasSuffix(query, ";") && !strings.HasPrefix(strings.ToLower(query), "use ") &&
			!strings.HasPrefix(strings.ToLower(query), "show ") &&
			!strings.HasPrefix(strings.ToLower(query), "desc") {
			continue // Wait for semicolon
		}

		if err := mysqlQuery(mc, query); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			return
		}
		multiLine.Reset()
	}
}
