package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
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

func proxyErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("Proxy error for %s %s: %v", r.Method, r.URL.Path, err)

	if errors.Is(err, context.DeadlineExceeded) {
		http.Error(w, "Gateway Timeout: upstream server took too long to respond", http.StatusGatewayTimeout)
		return
	}
	if errors.Is(err, context.Canceled) {
		http.Error(w, "Bad Gateway: request canceled or no upstream available", http.StatusBadGateway)
		return
	}
	http.Error(w, "Bad Gateway: upstream connection failed", http.StatusBadGateway)
}

type ProxyApp struct {
	Config            atomic.Pointer[ProxyConfig]
	AliveBackends     atomic.Pointer[[]*url.URL]
	RoundRobinCounter atomic.Uint64
	InFlight          atomic.Int64
	Cache             *sync.Map
	Visitors          *sync.Map
	DynamicTransport  *DynamicTransport
	WorkerManager     *WorkerManager
	Handler           http.Handler
	Server            *http.Server
	IdleConnsClosed   chan struct{}
}

func NewProxyApp(cfg *ProxyConfig) *ProxyApp {
	app := &ProxyApp{
		Cache:           new(sync.Map),
		Visitors:        new(sync.Map),
		WorkerManager:   NewWorkerManager(),
		IdleConnsClosed: make(chan struct{}),
	}
	app.Config.Store(cfg)

	transport := &DynamicTransport{}
	transport.current.Store(createTransport(cfg))
	app.DynamicTransport = transport

	dummyHost := url.URL{
		Scheme: "http",
		Host:   "localhost",
	}

	proxy := httputil.NewSingleHostReverseProxy(&dummyHost)
	myDirector := customDirector{
		originalDirector: proxy.Director,
		backendCounter:   &app.RoundRobinCounter,
		aliveBackends:    &app.AliveBackends,
	}

	proxy.Transport = app.DynamicTransport
	proxy.Director = myDirector.Direct
	proxy.ErrorHandler = proxyErrorHandler

	mux := http.NewServeMux()
	mux.Handle("/", recoveryMiddleware(
		limitClientConnections(
			checkHealth(
				rateLimit(
					cacheMiddleware(proxy, app.Cache, &app.Config),
					app.Visitors, &app.Config),
				&app.AliveBackends),
			&app.InFlight,
			&app.Config)))

	app.Handler = mux

	app.Server = &http.Server{
		Addr:              ":8081",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return app
}

func (app *ProxyApp) StartWorkers(configPath string) {
	app.WorkerManager.StartWorkers(
		configPath,
		&app.Config,
		&app.AliveBackends,
		app.DynamicTransport,
		app.Visitors,
		app.Cache,
	)
	go startSignalListener(app.Server, app.IdleConnsClosed)
}

func (app *ProxyApp) StopWorkers() {
	app.WorkerManager.StopWorkers()
}
