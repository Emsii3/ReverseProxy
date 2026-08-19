package main

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
)

type customDirector struct {
	aliveBackends    *atomic.Pointer[[]*url.URL]
	backendCounter   *atomic.Uint64
	originalDirector func(*http.Request)
}

func (c *customDirector) Direct(req *http.Request) {
	// adding headers
	clientAddr, _, err := net.SplitHostPort(req.RemoteAddr)
	if err == nil {
		req.Header.Set("X-Forwarded-For", clientAddr)
		req.Header.Set("X-Forwarded-Proto", "http")
		req.Header.Set("X-Real-IP", clientAddr)
	}

	//load balancer using round-robin
	backends := c.aliveBackends.Load()
	var idx int
	var count uint64

	if backends != nil && int64(len(*backends)) > 0 {
		count = c.backendCounter.Add(1)
		idx = int(int64(count) % int64(len(*backends)))
	} else {
		ctx, cancel := context.WithCancel(req.Context())
		cancel()
		*req = *req.WithContext(ctx)
		return
	}
	c.originalDirector(req)
	target := (*backends)[idx]
	req.URL.Scheme = target.Scheme
	req.Host = target.Host
	req.URL.Host = target.Host
}
