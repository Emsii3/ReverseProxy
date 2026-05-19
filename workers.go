package main

import (
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

func startHealthCheck(currentConfig *atomic.Pointer[ProxyConfig], isAlive *atomic.Bool) {
	for {
		cfg := currentConfig.Load()
		if len(cfg.Backends) > 0 {

			body, err := http.Get(cfg.Backends[0] + "/test")
			isAlive.Store(err == nil)
			if err == nil {
				body.Body.Close()
			}
			time.Sleep(time.Second * 10)
		} else {
			isAlive.Store(false)
			time.Sleep(time.Second * 10)
		}
	}
}
func startHotReloading(configPath string, currentConfig *atomic.Pointer[ProxyConfig]) {
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
func startRateLimit(visitors *sync.Map) {
	for {
		time.Sleep(time.Second * 3)
		visitors.Clear()
	}
}
func startClearingCache(cache *sync.Map) {
	for {
		time.Sleep(time.Minute * 10)
		cache.Range(func(k, v any) bool {
			box := v.(CachedResponse)
			if time.Now().After(box.ExpiresAt) {
				cache.Delete(k)
			}
			return true
		})
	}
}
