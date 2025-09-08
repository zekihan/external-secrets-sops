package redis

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
)

const defaultListenAddr = "localhost:6379"

func StartMockRedisServer(t *testing.T, addr string) error {
	t.Helper()

	listenAddr := addr
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}

	listenConfig := net.ListenConfig{}

	ln, err := listenConfig.Listen(t.Context(), "tcp", listenAddr)
	if err != nil {
		log.Errorf("listen error: %v", err)

		return fmt.Errorf("failed to start mock redis server: %w", err)
	}

	t.Cleanup(func() {
		err := ln.Close()
		if err != nil {
			log.Infof("error closing listener: %v", err)
		}
	})

	log.Infof("mock redis listening on %s\n", listenAddr)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}

				log.Infof("accept error: %v", err)

				continue
			}

			go handleConn(t, conn)
		}
	}()

	return nil
}

func handleConn(t *testing.T, conn net.Conn) {
	t.Helper()

	defer conn.Close()

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	for {
		cmd, args, err := readCommand(t, r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}

			log.Infof("read error: %v", err)

			return
		}

		switch strings.ToUpper(cmd) {
		case "PING":
			if len(args) > 0 {
				writeSimpleString(t, w, args[0])
			} else {
				writeSimpleString(t, w, "PONG")
			}
		case "SELECT", "CLIENT":
			writeSimpleString(t, w, "OK")
		case "QUIT":
			writeSimpleString(t, w, "OK")

			err = w.Flush()
			if err != nil {
				log.Infof("flush error: %v", err)
			}

			return
		default:
			// For unsupported commands respond with an error reply like Redis
			writeError(t, w, fmt.Sprintf("mock: unknown command '%s'", cmd))
		}

		err = w.Flush()
		if err != nil {
			log.Infof("flush error: %v", err)
		}
	}
}

// readCommand parses either inline commands or RESP arrays.
// It returns command (uppercased as provided), args slice, or an error.
func readCommand(t *testing.T, r *bufio.Reader) (string, []string, error) {
	t.Helper()

	// Peek the first byte to decide format
	b, err := r.Peek(1)
	if err != nil {
		return "", nil, fmt.Errorf("read error: %w", err)
	}

	if b[0] == '*' {
		return ifStar(t, r)
	}
	// Inline command: read a line, split by spaces
	line, err := readLine(t, r)
	if err != nil {
		return "", nil, err
	}

	if line == "" {
		return "", nil, nil
	}

	fields := strings.Fields(line)
	cmd := fields[0]
	args := fields[1:]

	return cmd, args, nil
}

var errInvalidArray = errors.New("invalid array format")

func ifStar(t *testing.T, r *bufio.Reader) (string, []string, error) {
	t.Helper()

	// RESP Array
	line, err := readLine(t, r)
	if err != nil {
		return "", nil, err
	}

	n, err := strconv.Atoi(strings.TrimPrefix(line, "*"))
	if err != nil || n <= 0 {
		return "", nil, fmt.Errorf("invalid array length %w", err)
	}

	parts := make([]string, 0, n)
	for range n {
		// expect $<len>\r\n
		desc, err := readLine(t, r)
		if err != nil {
			return "", nil, err
		}

		if !strings.HasPrefix(desc, "$") {
			return "", nil, errInvalidArray
		}

		ln, err := strconv.Atoi(strings.TrimPrefix(desc, "$"))
		if err != nil {
			return "", nil, fmt.Errorf("invalid bulk string length: %w", err)
		}

		buf := make([]byte, ln+2) // include CRLF

		_, err = io.ReadFull(r, buf)
		if err != nil {
			return "", nil, fmt.Errorf("read error: %w", err)
		}
		// trim trailing CRLF
		part := string(buf[:ln])
		parts = append(parts, part)
	}

	cmd := parts[0]
	args := parts[1:]

	return cmd, args, nil
}

func readLine(t *testing.T, r *bufio.Reader) (string, error) {
	t.Helper()

	line, err := r.ReadBytes('\n')
	if err != nil {
		return "", fmt.Errorf("read error: %w", err)
	}
	// line ends with \n; strip trailing CRLF or LF
	line = bytes.TrimRight(line, "\r\n")

	return string(line), nil
}

func writeSimpleString(t *testing.T, w *bufio.Writer, str string) {
	t.Helper()

	_, err := w.WriteString("+")
	if err != nil {
		log.Infof("write error: %v", err)

		return
	}

	_, err = w.WriteString(str)
	if err != nil {
		log.Infof("write error: %v", err)

		return
	}

	_, err = w.WriteString("\r\n")
	if err != nil {
		log.Infof("write error: %v", err)

		return
	}
}

func writeError(t *testing.T, w *bufio.Writer, msg string) {
	t.Helper()

	_, err := w.WriteString("-")
	if err != nil {
		log.Infof("write error: %v", err)

		return
	}

	_, err = w.WriteString(msg)
	if err != nil {
		log.Infof("write error: %v", err)

		return
	}

	_, err = w.WriteString("\r\n")
	if err != nil {
		log.Infof("write error: %v", err)

		return
	}
}
