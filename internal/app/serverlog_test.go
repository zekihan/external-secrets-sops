package app

import (
	"bytes"
	"testing"
)

func TestServerLogWriter(t *testing.T) {
	var output bytes.Buffer
	writer := serverLogWriter{output: &output}
	messages := []struct {
		message string
		keep    bool
	}{
		{"2026/10/27 00:00:00 http: TLS handshake error from 10.42.16.4:1234: EOF\n", false},
		{
			"2026/10/27 00:00:00 http: TLS handshake error from 10.42.16.4:1234: tls: bad certificate\n",
			true,
		},
		{"2026/10/27 00:00:00 http: panic serving 10.42.16.4:1234: EOF\n", true},
	}
	for _, tc := range messages {
		output.Reset()
		n, err := writer.Write([]byte(tc.message))
		if err != nil || n != len(tc.message) {
			t.Fatalf("Write() = %d, %v", n, err)
		}
		if got := output.String(); (got != "") != tc.keep || (tc.keep && got != tc.message) {
			t.Errorf("Write(%q) logged %q", tc.message, got)
		}
	}
}
