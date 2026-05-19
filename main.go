package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"sync"
	"sync/atomic"
)

func main() {
	// program initialization
	configPath := "config.json"
	cache := new(sync.Map)    // cache
	visitors := new(sync.Map) // rate limiter clearing
	var currentConfig atomic.Pointer[ProxyConfig]
	currentConfig.Store(reloadConfig(configPath))
	cfg := currentConfig.Load()
	var isAlive atomic.Bool
	var roundRobinCounter atomic.Uint64
	// start workers
	go startConfigWatcher(configPath, &currentConfig) // reload config every 5 seconds
	go startVisitorCleaner(visitors)                  // rate limiter reset
	go startHealthCheck(&currentConfig, &isAlive)     // check if services are alive
	go startCacheCleaner(cache)                       // cache clearing
	// start services
	proxy := httputil.NewSingleHostReverseProxy(cfg.parsedURLs[0]) // this is fine only because director is choosing correct adress to sent requests to. This line is here only to create reverseproxy.
	myDirector := customDirector{
		originalDirector: proxy.Director,
		backendCounter:   &roundRobinCounter,
		currentConfig:    &currentConfig,
	}
	proxy.Director = myDirector.Direct

	log.Println("Initialization successful")
	http.Handle("/", checkHealth(rateLimit(cacheMiddleware(proxy, cache, &currentConfig), visitors, &currentConfig), &isAlive))
	http.ListenAndServe(":8081", nil)
}
