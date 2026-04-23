package db

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadRESP_SimpleString(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("+OK\r\n"))
	v, err := readRESP(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.typ != '+' || v.str != "OK" {
		t.Errorf("unexpected %+v", v)
	}
}

func TestReadRESP_Error(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("-ERR unknown\r\n"))
	v, err := readRESP(r)
	if err != nil {
		t.Fatal(err)
	}
	if v.typ != '-' || v.str != "ERR unknown" {
		t.Errorf("unexpected %+v", v)
	}
}

func TestReadRESP_BulkString(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("$5\r\nhello\r\n"))
	v, err := readRESP(r)
	if err != nil {
		t.Fatal(err)
	}
	if v.typ != '$' || v.str != "hello" {
		t.Errorf("unexpected %+v", v)
	}
}

func TestReadRESP_BulkNil(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("$-1\r\n"))
	v, err := readRESP(r)
	if err != nil {
		t.Fatal(err)
	}
	if !v.isNil {
		t.Errorf("expected nil bulk, got %+v", v)
	}
}

func TestReadRESP_Array(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("*2\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"))
	v, err := readRESP(r)
	if err != nil {
		t.Fatal(err)
	}
	if v.typ != '*' || len(v.array) != 2 {
		t.Fatalf("unexpected %+v", v)
	}
	if v.array[0].str != "foo" || v.array[1].str != "bar" {
		t.Errorf("bad contents: %+v", v.array)
	}
}

// Regression: prior code did `length, _ := strconv.Atoi(data)` — a server
// sending `$99999999999` would try to allocate ~100GB.
func TestReadRESP_RejectHugeBulk(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("$99999999999\r\n"))
	_, err := readRESP(r)
	if err == nil {
		t.Fatal("expected error for oversize bulk")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("expected 'out of range' in error, got %v", err)
	}
}

func TestReadRESP_RejectHugeArray(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("*99999999\r\n"))
	_, err := readRESP(r)
	if err == nil {
		t.Fatal("expected error for oversize array")
	}
}

func TestReadRESP_RejectMalformedLength(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("$abc\r\nhi\r\n"))
	_, err := readRESP(r)
	if err == nil {
		t.Fatal("expected error for malformed length")
	}
}

func TestReadRESP_EmptyLine(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("\r\n"))
	_, err := readRESP(r)
	if err == nil {
		t.Fatal("expected error for empty line")
	}
}

func TestReadRESP_Integer(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(":42\r\n"))
	v, err := readRESP(r)
	if err != nil {
		t.Fatal(err)
	}
	if v.typ != ':' || v.integer != 42 {
		t.Errorf("unexpected %+v", v)
	}
}
