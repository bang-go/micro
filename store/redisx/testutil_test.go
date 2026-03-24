package redisx

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
)

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type fakeRedisServer struct {
	mu       sync.Mutex
	store    map[string]string
	commands [][]string
}

func newFakeRedisServer() *fakeRedisServer {
	return &fakeRedisServer{
		store: make(map[string]string),
	}
}

func (s *fakeRedisServer) dialer(_ context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	go s.serve(server)
	return client, nil
}

func (s *fakeRedisServer) serve(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	for {
		cmd, err := readRESPArray(reader)
		if err != nil {
			if err != io.EOF {
				return
			}
			return
		}
		if len(cmd) == 0 {
			return
		}

		s.mu.Lock()
		s.commands = append(s.commands, append([]string(nil), cmd...))
		s.mu.Unlock()

		switch strings.ToUpper(cmd[0]) {
		case "PING":
			_ = writeSimpleString(writer, "PONG")
		case "SET":
			if len(cmd) >= 3 {
				s.mu.Lock()
				s.store[cmd[1]] = cmd[2]
				s.mu.Unlock()
			}
			_ = writeSimpleString(writer, "OK")
		case "GET":
			var value string
			var ok bool
			if len(cmd) >= 2 {
				s.mu.Lock()
				value, ok = s.store[cmd[1]]
				s.mu.Unlock()
			}
			if !ok {
				_ = writeNilBulkString(writer)
			} else {
				_ = writeBulkString(writer, value)
			}
		case "DEL":
			deleted := 0
			s.mu.Lock()
			for _, key := range cmd[1:] {
				if _, ok := s.store[key]; ok {
					delete(s.store, key)
					deleted++
				}
			}
			s.mu.Unlock()
			_ = writeInteger(writer, deleted)
		default:
			_ = writeError(writer, "ERR unknown command")
		}
		if err := writer.Flush(); err != nil {
			return
		}
	}
}

func (s *fakeRedisServer) commandLog() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := make([][]string, len(s.commands))
	for i, cmd := range s.commands {
		cloned[i] = append([]string(nil), cmd...)
	}
	return cloned
}

func readRESPArray(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if line == "" {
		return nil, io.EOF
	}
	if line[0] != '*' {
		return nil, fmt.Errorf("unexpected resp prefix: %q", line)
	}
	count, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, count)
	for i := 0; i < count; i++ {
		sizeLine, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if sizeLine == "" || sizeLine[0] != '$' {
			return nil, fmt.Errorf("unexpected bulk prefix: %q", sizeLine)
		}
		size, err := strconv.Atoi(strings.TrimSpace(sizeLine[1:]))
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		result = append(result, string(buf[:size]))
	}
	return result, nil
}

func writeSimpleString(w *bufio.Writer, value string) error {
	_, err := fmt.Fprintf(w, "+%s\r\n", value)
	return err
}

func writeBulkString(w *bufio.Writer, value string) error {
	_, err := fmt.Fprintf(w, "$%d\r\n%s\r\n", len(value), value)
	return err
}

func writeNilBulkString(w *bufio.Writer) error {
	_, err := io.WriteString(w, "$-1\r\n")
	return err
}

func writeInteger(w *bufio.Writer, value int) error {
	_, err := fmt.Fprintf(w, ":%d\r\n", value)
	return err
}

func writeError(w *bufio.Writer, message string) error {
	_, err := fmt.Fprintf(w, "-%s\r\n", message)
	return err
}

func boolPtr(value bool) *bool {
	return &value
}
