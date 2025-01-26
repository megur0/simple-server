package server

import (
	"errors"
	"testing"
)

// go test -v -count=1 -timeout 60s -run ^TestError$ ./server
func TestError(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		if !errors.As(&ErrRequestJsonSomethingInvalid{
			Err: &ErrRequestFieldFormat{},
		}, ptr(&ErrRequestFieldFormat{})) {
			t.Error("unexpected result")
		}
		if !errors.As(&ErrRequestFieldFormat{
			Err: &ErrRequestJsonSomethingInvalid{},
		}, ptr(&ErrRequestJsonSomethingInvalid{})) {
			t.Error("unexpected result")
		}
		if !errors.As(&ErrRequestJsonSyntaxError{
			Err: &ErrRequestJsonSomethingInvalid{},
		}, ptr(&ErrRequestJsonSomethingInvalid{})) {
			t.Error("unexpected result")
		}
	})
}
