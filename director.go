package main

import (
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
	c.originalDirector(req)
	// adding headers
	clientAddr := req.RemoteAddr
	req.Header.Set("X-Forwarded-For", clientAddr)
	req.Header.Add("X-Forwarded-Proto", "http")
	req.Header.Set("X-Real-IP", req.RemoteAddr)

	//load balancer using round-robin
	backends := c.aliveBackends.Load()
	count := c.backendCounter.Add(1)
	var idx int
	if backends != nil && int64(len(*backends)) > 0 {
		idx = int(int64(count) % int64(len(*backends)))
	} else {
		return
	}
	target := (*backends)[idx]
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
}
