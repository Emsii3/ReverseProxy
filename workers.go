package main

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

func checkHealthOnce(myClient *http.Client, currentConfig *atomic.Pointer[ProxyConfig], aliveBackends *atomic.Pointer[[]*url.URL]) {
	var aliveURLs []*url.URL
	cfg := currentConfig.Load()
	if cfg != nil && len(cfg.Backends) > 0 {
		for _, element := range cfg.Backends {
			endpoint, err := url.JoinPath(element, "test")
			if err != nil {
				continue
			}
			body, err := myClient.Get(endpoint)
			if err != nil {
				continue
			}
			io.Copy(io.Discard, body.Body)
			body.Body.Close()
			if body.StatusCode < 200 || body.StatusCode > 299 {
				continue
			}
			parsed, err := url.Parse(element)
			if err != nil {
				continue
			}
			aliveURLs = append(aliveURLs, parsed)
		}
	}
	aliveBackends.Store(&aliveURLs)
}

func startHealthCheck(ctx context.Context, currentConfig *atomic.Pointer[ProxyConfig], aliveBackends *atomic.Pointer[[]*url.URL]) {
	myClient := http.Client{
		Timeout: time.Second * 2,
	}

	checkHealthOnce(&myClient, currentConfig, aliveBackends)

	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkHealthOnce(&myClient, currentConfig, aliveBackends)
		}
	}
}

func startConfigWatcher(ctx context.Context, configPath string, currentConfig *atomic.Pointer[ProxyConfig], dynamicTransport *DynamicTransport) {
	fileinfo, err := os.Stat(configPath)
	var lastMod time.Time
	if err == nil {
		lastMod = fileinfo.ModTime()
	}

	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fileinfo, err := os.Stat(configPath)
			if err == nil && fileinfo.ModTime().After(lastMod) {
				config := reloadConfig(configPath)
				if config != nil && len(config.Backends) > 0 {
					currentConfig.Store(config)
					if dynamicTransport != nil {
						dynamicTransport.current.Store(createTransport(config))
					}
					lastMod = fileinfo.ModTime()
				}
			}
		}
	}
}

func startVisitorCleaner(ctx context.Context, visitors *sync.Map) {
	ticker := time.NewTicker(time.Second * 3)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			visitors.Clear()
		}
	}
}

func cleanExpiredCache(cache *sync.Map) {
	now := time.Now()
	cache.Range(func(k, v any) bool {
		entry, ok := v.(CachedResponse)
		if ok && now.After(entry.ExpiresAt) {
			cache.Delete(k)
		}
		return true
	})
}

func startCacheCleaner(ctx context.Context, cache *sync.Map) {
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanExpiredCache(cache)
		}
	}
}

type WorkerManager struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewWorkerManager() *WorkerManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerManager{
		ctx:    ctx,
		cancel: cancel,
	}
}

func (wm *WorkerManager) StartWorkers(
	configPath string,
	currentConfig *atomic.Pointer[ProxyConfig],
	aliveBackends *atomic.Pointer[[]*url.URL],
	dynamicTransport *DynamicTransport,
	visitors *sync.Map,
	cache *sync.Map,
) {
	wm.wg.Add(4)

	go func() {
		defer wm.wg.Done()
		startConfigWatcher(wm.ctx, configPath, currentConfig, dynamicTransport)
	}()

	go func() {
		defer wm.wg.Done()
		startVisitorCleaner(wm.ctx, visitors)
	}()

	go func() {
		defer wm.wg.Done()
		startHealthCheck(wm.ctx, currentConfig, aliveBackends)
	}()

	go func() {
		defer wm.wg.Done()
		startCacheCleaner(wm.ctx, cache)
	}()
}

func (wm *WorkerManager) StopWorkers() {
	wm.cancel()
	wm.wg.Wait()
}
