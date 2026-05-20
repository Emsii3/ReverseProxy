package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	var roundRobinCounter atomic.Uint64
	var aliveBackends atomic.Pointer[[]*url.URL]

	// start workers
	go startConfigWatcher(configPath, &currentConfig)   // reload config every 5 seconds
	go startVisitorCleaner(visitors)                    // rate limiter reset
	go startHealthCheck(&currentConfig, &aliveBackends) // check if services are alive
	go startCacheCleaner(cache)                         // cache clearing

	// start services
	proxy := httputil.NewSingleHostReverseProxy(cfg.parsedURLs[0]) // this is fine only because director is choosing correct adress to sent requests to. This line is here only to create reverseproxy.
	myDirector := customDirector{
		originalDirector: proxy.Director,
		backendCounter:   &roundRobinCounter,
		aliveBackends:    &aliveBackends,
	}
	proxy.Director = myDirector.Direct
	log.Println("Initialization successful")
	http.Handle("/", checkHealth(rateLimit(cacheMiddleware(proxy, cache, &currentConfig), visitors, &currentConfig), &aliveBackends))
	http.ListenAndServe(":8081", nil)
}
