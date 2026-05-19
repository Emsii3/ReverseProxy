package main

import (
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

func rateLimit(next http.Handler, visitors *sync.Map, config *atomic.Pointer[ProxyConfig]) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := config.Load()
		rateLimitMax := int64(cfg.RateLimitMax)
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		var timesVisited = new(atomic.Int64)
		if err != nil {
			http.Error(w, "Internal server error", 500)
			return
		}
		val, _ := visitors.LoadOrStore(ip, timesVisited)
		timesVisitedFromMap := val.(*atomic.Int64)
		currentVal := timesVisitedFromMap.Add(1)
		if currentVal > rateLimitMax {
			http.Error(w, "Too many requests", 429)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func checkHealth(next http.Handler, isAlive *atomic.Bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAlive.Load() == false {
			http.Error(w, "The server was acting as a gateway or proxy and did not receive a timely response from the upstream server.", 504)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func cacheMiddleware(next http.Handler, cache *sync.Map, config *atomic.Pointer[ProxyConfig]) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := config.Load()
		cacheable := cfg.CacheRules
		if allowCache, exists := cacheable[r.URL.Path]; !exists || !allowCache {
			next.ServeHTTP(w, r)
			return
		}
		//cacheing
		key := r.Method + ":" + r.URL.String()
		if val, ok := cache.Load(key); ok == true {
			box := val.(CachedResponse)
			for k, v := range box.Headers {
				w.Header()[k] = v
			}
			w.WriteHeader(box.StatusCode)
			w.Write(box.Body)
			if time.Now().After(box.ExpiresAt) {
				cache.Delete(key)
			}
			return
		} else {
			rr := newResponseRecorder(w)
			next.ServeHTTP(rr, r)
			response := CachedResponse{
				StatusCode: rr.statusCode,
				Body:       rr.buffer.Bytes(),
				Headers:    rr.Header().Clone(),
				ExpiresAt:  time.Now().Add(time.Minute * 1),
			}
			cache.Store(key, response)
		}
	})
}

func main() {
	configPath := "config.json"
	cache := new(sync.Map)    // cache
	visitors := new(sync.Map) // rate limiter clearing
	var currentConfig atomic.Pointer[ProxyConfig]
	loadConfig(configPath, &currentConfig) // load config once on start
	cfg := currentConfig.Load()
	go func() {
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
	}() // reload config every 5 seconds

	go func() {
		for {
			time.Sleep(time.Second * 3)
			visitors.Clear()
		}
	}() // rate limiter reset

	var isAlive atomic.Bool // healthcheck
	go func() {
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
	}()

	go func() {
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
	}() // cache clearing

	var backendToChoose atomic.Uint64
	proxy := httputil.NewSingleHostReverseProxy(cfg.parsedURLs[0]) // this should be placed elsewhere
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// adding headers
		clientAddr := req.RemoteAddr
		req.Header.Set("X-Forwarded-For", clientAddr)
		req.Header.Add("X-Forwarded-Proto", "http")
		req.Header.Set("X-Real-IP", req.RemoteAddr)

		//load balancer using round-robin
		tempCfg := currentConfig.Load()
		count := backendToChoose.Add(1)
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

	println("Magia")
	http.Handle("/", checkHealth(rateLimit(cacheMiddleware(proxy, cache, &currentConfig), visitors, &currentConfig), &isAlive))
	http.ListenAndServe(":8081", nil)
}
