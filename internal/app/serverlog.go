package app

import (
	"bytes"
	"io"
	"log"
)

type serverLogWriter struct {
	output io.Writer
}

func (w serverLogWriter) Write(p []byte) (int, error) {
	message := bytes.TrimSpace(p)
	const timestampLength = len("2006/01/02 15:04:05 ")
	if len(message) > timestampLength &&
		bytes.HasPrefix(message[timestampLength:], []byte("http: TLS handshake error from ")) &&
		bytes.HasSuffix(message, []byte(": EOF")) {
		return len(p), nil
	}
	return w.output.Write(p)
}

func serverErrorLog() *log.Logger {
	return log.New(serverLogWriter{output: log.Default().Writer()}, "", log.LstdFlags)
}
