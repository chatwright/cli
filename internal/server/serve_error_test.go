package server

import (
	"errors"
	"net/http"
	"testing"
)

func TestUnexpectedServeError(t *testing.T) {
	if unexpectedServeError(http.ErrServerClosed) != nil || unexpectedServeError(nil) != nil {
		t.Error("graceful close reported as an error")
	}
	boom := errors.New("boom")
	if !errors.Is(unexpectedServeError(boom), boom) {
		t.Error("real serve error dropped")
	}
}
