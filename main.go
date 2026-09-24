package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
)

type DynamicTransport struct {
	current atomic.Pointer[http.Transport]
}

func (d *DynamicTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return d.current.Load().RoundTrip(req)
}

func createTransport(cfg *ProxyConfig) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxConnsPerHost = cfg.MaxConnsPerHost
	t.MaxIdleConnsPerHost = cfg.MaxIdleConnsPerHost
	return t
}

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

	var inFlight atomic.Int64
	var dynamicTransport DynamicTransport
	dynamicTransport.current.Store(createTransport(cfg))

	srv := &http.Server{
		Addr:    ":8081",
		Handler: http.DefaultServeMux,
	}

	idleConnsClosed := make(chan struct{}) // graceful shutdown init

	//load balancer setup
	var roundRobinCounter atomic.Uint64
	var aliveBackends atomic.Pointer[[]*url.URL]
	dummyHost := url.URL{
		Scheme: "http",
		Host:   "localhost",
	}

	// start workers
	go startConfigWatcher(configPath, &currentConfig, &dynamicTransport) // reload config every 5 seconds
	go startVisitorCleaner(visitors)                                   // rate limit reset
	go startHealthCheck(&currentConfig, &aliveBackends)                // check if services are alive
	go startCacheCleaner(cache)                                        // clear carche
	go startSignalListener(srv, idleConnsClosed)                       // listen for signals

	proxy := httputil.NewSingleHostReverseProxy(&dummyHost) // this is fine only because director is choosing correct adress to sent requests to. This line is here only to create reverseproxy.
	myDirector := customDirector{
		originalDirector: proxy.Director,
		backendCounter:   &roundRobinCounter,
		aliveBackends:    &aliveBackends,
	}

	proxy.Transport = &dynamicTransport
	proxy.Director = myDirector.Direct
	log.Println("Initialization successful")

	http.Handle("/", limitClientConnections(
		checkHealth(
			rateLimit(
				cacheMiddleware(proxy, cache, &currentConfig),
				visitors, &currentConfig),
			&aliveBackends),
		&inFlight,
		&currentConfig))

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		// Error starting or closing listener:
		log.Fatalf("HTTP server ListenAndServe: %v", err)
	}

	<-idleConnsClosed

	log.Println("Shutdown successful. Quiting program")
}
