package qfnu

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

type closeErrorBody struct {
	*strings.Reader
}

func (closeErrorBody) Close() error {
	return errors.New("close failed")
}

func TestReadResponseBodyReturnsCloseError(t *testing.T) {
	response := &http.Response{Body: closeErrorBody{Reader: strings.NewReader("data")}}

	_, err := readResponseBody(response)

	if err == nil || !strings.Contains(err.Error(), "close HTTP response body") {
		t.Fatalf("readResponseBody() error = %v", err)
	}
}
