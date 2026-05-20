package main

import (
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

func startHealthCheck(currentConfig *atomic.Pointer[ProxyConfig], aliveBackends *atomic.Pointer[[]*url.URL]) {
	myClient := http.Client{
		Timeout: time.Second * 2,
	}
	for {
		var aliveURLs []*url.URL
		cfg := currentConfig.Load()
		if len(cfg.Backends) > 0 {
			for _, element := range cfg.Backends {
				endpoint, err := url.JoinPath(element, "test")
				if err != nil {
					continue
				}
				body, err := myClient.Get(endpoint)
				if err != nil {
					continue
				}
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
		time.Sleep(time.Second * 10)
	}
}

func startConfigWatcher(configPath string, currentConfig *atomic.Pointer[ProxyConfig]) {
	fileinfo, _ := os.Stat(configPath)
	lastMod := fileinfo.ModTime()
	for {
		time.Sleep(time.Second * 5)
		fileinfo, err := os.Stat(configPath)
		if err == nil && fileinfo.ModTime().After(lastMod) {
			config := reloadConfig(configPath)
			if config != nil && len(config.Backends) > 0 {
				currentConfig.Store(config)
				lastMod = fileinfo.ModTime()
			}

		}
	}
}
func startVisitorCleaner(visitors *sync.Map) {
	for {
		time.Sleep(time.Second * 3)
		visitors.Clear()
	}
}
func startCacheCleaner(cache *sync.Map) {
	for {
		time.Sleep(time.Minute * 10)
		cache.Range(func(k, v any) bool {
			entry := v.(CachedResponse)
			if time.Now().After(entry.ExpiresAt) {
				cache.Delete(k)
			}
			return true
		})
	}
}
