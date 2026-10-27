package offline

import "os"

func cancellableFile(file *os.File) (*os.File, error) {
	return file, nil
}
