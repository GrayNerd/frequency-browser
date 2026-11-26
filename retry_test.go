package main

import (
	"errors"
	"io"
	"syscall"
	"testing"
)

// TestIsTransientRigError pins the retry policy: connection-level failures are
// retried, while a rig error reply is a real answer and must be returned at once.
func TestIsTransientRigError(t *testing.T) {
	transient := []error{
		io.EOF,
		io.ErrUnexpectedEOF,
		syscall.ECONNRESET,
		syscall.ECONNREFUSED,
		syscall.EPIPE,
	}
	for _, err := range transient {
		if !isTransientRigError(err) {
			t.Errorf("isTransientRigError(%v) = false, want true", err)
		}
	}

	permanent := []error{
		nil,
		errors.New("rig error: -8 (frequency out of the radio's tuning range)"),
		errors.New("failed to parse frequency: strconv.Atoi"),
	}
	for _, err := range permanent {
		if isTransientRigError(err) {
			t.Errorf("isTransientRigError(%v) = true, want false", err)
		}
	}
}
