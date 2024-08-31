package routes

import (
	"errors"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
)

func Write(w http.ResponseWriter, b []byte) bool {
	_, err := w.Write(b)
	return CheckError(w, err, http.StatusInternalServerError, "failed to write string")
}

func WriteString(w http.ResponseWriter, s string) bool {
	_, err := io.WriteString(w, s)
	return CheckError(w, err, http.StatusInternalServerError, "failed to write string")
}

// CheckError checks the error, and writes to w if there is an error.  Returns
// true if err is non-nil
func CheckError(w http.ResponseWriter, err error, statusCode int, messages ...string) bool {
	if err != nil {
		err = wrapErrors(err, messages...)
		if statusCode == 0 {
			statusCode = http.StatusInternalServerError
		}
		if statusCode >= 500 {
			log.Error(err)
		} else {
			log.Warn(err)
		}
		http.Error(w, err.Error(), statusCode)
		return true
	}
	return false
}

func wrapErrors(err error, messages ...string) error {
	if len(messages) == 0 {
		return err
	}
	errs := make([]error, len(messages)+1)
	errs[0] = err
	for i := 0; i < len(messages); i++ {
		errs[i+1] = errors.New(messages[i])
	}
	return errors.Join(errs...)
}
