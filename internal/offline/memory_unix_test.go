//go:build !windows

package offline

import (
	"os"
	"testing"
)

func TestCancellableFileClosed(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if stream, err := cancellableFile(file); err == nil {
		_ = stream.Close()
		t.Fatal("closed file accepted")
	}
}
