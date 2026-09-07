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
	cfg := reloadConfig(configPath)
	if cfg == nil {
		log.Fatal("cannot start: invalid or missing config.json")
	}
	currentConfig.Store(cfg)

	//load balancer setup
	var roundRobinCounter atomic.Uint64
	var aliveBackends atomic.Pointer[[]*url.URL]
	dummyHost := url.URL{
		Scheme: "http",
		Host:   "localhost",
	}

	// start workers
	go startConfigWatcher(configPath, &currentConfig)   // reload config every 5 seconds
	go startVisitorCleaner(visitors)                    // rate limit reset
	go startHealthCheck(&currentConfig, &aliveBackends) // check if services are alive
	go startCacheCleaner(cache)                         // clear carche

	proxy := httputil.NewSingleHostReverseProxy(&dummyHost) // this is fine only because director is choosing correct adress to sent requests to. This line is here only to create reverseproxy.
	myDirector := customDirector{
		originalDirector: proxy.Director,
		backendCounter:   &roundRobinCounter,
		aliveBackends:    &aliveBackends,
	}

	myTransport := http.DefaultTransport.(*http.Transport).Clone()
	myTransport.MaxConnsPerHost = 150
	myTransport.MaxIdleConnsPerHost = 150

	proxy.Transport = myTransport

	proxy.Director = myDirector.Direct
	log.Println("Initialization successful")
	http.Handle("/", checkHealth(
		rateLimit(
			cacheMiddleware(proxy, cache, &currentConfig),
			visitors, &currentConfig),
		&aliveBackends))
	http.ListenAndServe(":8081", nil)
}
