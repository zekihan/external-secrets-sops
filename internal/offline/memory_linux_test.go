//go:build linux

package offline

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCancellableFileUnpollableDescriptor(t *testing.T) {
	fd, err := unix.Open(t.TempDir(), unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), "path-only")
	defer func() { _ = file.Close() }()
	if stream, err := cancellableFile(file); err == nil {
		_ = stream.Close()
		t.Fatal("path-only descriptor accepted")
	}
}
