package main

import (
	"net"
	"net/http"
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
			entry := val.(CachedResponse)
			for k, v := range entry.Headers {
				w.Header()[k] = v
			}
			w.WriteHeader(entry.StatusCode)
			w.Write(entry.Body)
			if time.Now().After(entry.ExpiresAt) {
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
