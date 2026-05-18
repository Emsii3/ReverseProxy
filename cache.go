package main

import (
	"bytes"
	"net/http"
	"time"
)

type CachedResponse struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
	ExpiresAt  time.Time
}

type responseRecorder struct {
	response   http.ResponseWriter
	statusCode int
	buffer     *bytes.Buffer
}

func (rr *responseRecorder) Header() http.Header {
	return rr.response.Header()
}

func (rr *responseRecorder) WriteHeader(statusCode int) {
	rr.response.WriteHeader(statusCode)
	rr.statusCode = statusCode
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	rr.buffer.Write(b)
	return rr.response.Write(b)
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{
		response:   w,
		statusCode: 200,
		buffer:     new(bytes.Buffer),
	}
}
