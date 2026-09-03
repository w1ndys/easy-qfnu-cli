package qfnu

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// readResponseBody consumes and closes an HTTP response. Read and close errors
// are returned instead of being silently discarded.
func readResponseBody(response *http.Response) ([]byte, error) {
	data, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read HTTP response body: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close HTTP response body: %w", closeErr)
	}
	return data, nil
}

// decodeResponseJSON decodes one JSON value, drains the remaining bytes for
// connection reuse, and always closes the response.
func decodeResponseJSON(response *http.Response, target any) error {
	decodeErr := json.NewDecoder(response.Body).Decode(target)
	_, drainErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if decodeErr != nil {
		return fmt.Errorf("decode HTTP response body: %w", decodeErr)
	}
	if drainErr != nil {
		return fmt.Errorf("drain HTTP response body: %w", drainErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close HTTP response body: %w", closeErr)
	}
	return nil
}

// discardResponseBody drains without retaining bytes so the transport can
// reuse the connection, then reports either the read or close failure.
func discardResponseBody(response *http.Response) error {
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		return fmt.Errorf("discard HTTP response body: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close HTTP response body: %w", closeErr)
	}
	return nil
}
