package main

import (
	"net/http"
	"net/http/httputil"
	"sync"
	"sync/atomic"
)

func main() {
	configPath := "config.json"
	cache := new(sync.Map)    // cache
	visitors := new(sync.Map) // rate limiter clearing
	var currentConfig atomic.Pointer[ProxyConfig]
	currentConfig.Store(reloadConfig(configPath))
	cfg := currentConfig.Load()
	var isAlive atomic.Bool

	go startHotReloading(configPath, &currentConfig) // reload config every 5 seconds
	go startRateLimit(visitors)                      // rate limiter reset
	go startHealthCheck(&currentConfig, &isAlive)
	go startClearingCache(cache) // cache clearing

	var backendToChoose atomic.Uint64
	proxy := httputil.NewSingleHostReverseProxy(cfg.parsedURLs[0]) // this is fine only because director is choosing correct adress to sent requests to. This line is here only to create reverseproxy.
	myDirector := customDirector{
		originalDirector: proxy.Director,
		backendCounter:   &backendToChoose,
		currentConfig:    &currentConfig,
	}
	proxy.Director = myDirector.Direct

	println("Magia")
	http.Handle("/", checkHealth(rateLimit(cacheMiddleware(proxy, cache, &currentConfig), visitors, &currentConfig), &isAlive))
	http.ListenAndServe(":8081", nil)
}
