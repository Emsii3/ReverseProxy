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
	clientAddr, _, err := net.SplitHostPort(req.RemoteAddr)
	if err == nil {
		req.Header.Set("X-Forwarded-For", clientAddr)
		req.Header.Set("X-Forwarded-Proto", "http")
		req.Header.Set("X-Real-IP", clientAddr)
	}

	backends := c.aliveBackends.Load()
	if backends == nil || len(*backends) == 0 {
		ctx, cancel := context.WithCancel(req.Context())
		cancel()
		*req = *req.WithContext(ctx)
		return
	}

	c.originalDirector(req)
	count := c.backendCounter.Add(1)
	idx := int(int64(count) % int64(len(*backends)))
	target := (*backends)[idx]
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	req.Host = target.Host
}
