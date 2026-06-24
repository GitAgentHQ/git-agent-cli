package cmd

import (
	"errors"
	"testing"
)

func TestOutputError_JSONModeReturnsError(t *testing.T) {
	err := errors.New("boom")
	if got := outputError(true, false, err); got != err {
		t.Fatalf("outputError(json) = %v, want %v", got, err)
	}
}

func TestOutputError_TextModeReturnsError(t *testing.T) {
	err := errors.New("boom")
	if got := outputError(false, true, err); got != err {
		t.Fatalf("outputError(text) = %v, want %v", got, err)
	}
}
