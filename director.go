package main

import (
	"net/http"
	"sync/atomic"
)

type customDirector struct {
	currentConfig    *atomic.Pointer[ProxyConfig]
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
	tempCfg := c.currentConfig.Load()
	count := c.backendCounter.Add(1)
	var idx int
	if int64(len(tempCfg.parsedURLs)) > 0 {
		idx = int(int64(count) % int64(len(tempCfg.parsedURLs)))
	} else {
		return
	}
	target := tempCfg.parsedURLs[idx]
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
}
