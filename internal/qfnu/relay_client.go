package qfnu

import (
	"net/http"
	"time"
)

type relayClientError struct {
	readingResponse bool
}

func doRelayRequest(request *http.Request) (int, []byte, *relayClientError) {
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return 0, nil, &relayClientError{}
	}
	data, err := readResponseBody(response)
	if err != nil {
		return 0, nil, &relayClientError{readingResponse: true}
	}
	return response.StatusCode, data, nil
}
