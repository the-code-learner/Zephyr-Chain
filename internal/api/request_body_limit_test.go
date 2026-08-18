package api

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestReadAndRestoreRequestBodyRejectsOversizedPeerBody(t *testing.T) {
	body := bytes.Repeat([]byte{'x'}, int(maxPublicRequestBodyBytes)+1)
	request := httptest.NewRequest("POST", "/v1/internal/blocks", bytes.NewReader(body))

	if _, err := readAndRestoreRequestBody(request); !errors.Is(err, errRequestBodyTooLarge) {
		t.Fatalf("expected oversized peer body rejection, got %v", err)
	}
}
