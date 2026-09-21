package testutil

import (
	"net"
	"os/exec"
	"strconv"
	"sync"
	"testing"
)

func FreeAddress(t testing.TB) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := ln.Addr().String()
	if err = ln.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}
func StartRedis(t testing.TB, address string) func() {
	t.Helper()
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Fatal("redis-server is required for integration tests")
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = strconv.Atoi(port); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(
		binary,
		"--bind",
		"127.0.0.1",
		"--port",
		port,
		"--save",
		"",
		"--appendonly",
		"no",
		"--dir",
		t.TempDir(),
	)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() { once.Do(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }) }
	t.Cleanup(stop)
	return stop
}
