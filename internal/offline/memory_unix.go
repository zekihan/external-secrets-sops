//go:build !windows

package offline

import (
	"os"

	"golang.org/x/sys/unix"
)

func cancellableFile(file *os.File) (*os.File, error) {
	fd, err := unix.Dup(int(file.Fd()))
	if err != nil {
		return nil, err
	}
	// NewFile registers nonblocking pipes with the runtime poller so Close interrupts Read.
	if err = unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	unix.CloseOnExec(fd)
	return os.NewFile(uintptr(fd), "memory-input"), nil
}
