package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkCheckHealth_Alive(b *testing.B) {
	var isAlive atomic.Bool
	isAlive.Store(true)
	handler := checkHealth(okHandler(), &isAlive)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkRateLimit_UnderLimit(b *testing.B) {
	visitors := new(sync.Map)
	config := makeConfig(1<<31, nil)
	handler := rateLimit(okHandler(), visitors, config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkRateLimit_ManyIPs(b *testing.B) {
	visitors := new(sync.Map)
	config := makeConfig(1<<31, nil)
	handler := rateLimit(okHandler(), visitors, config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = fmt.Sprintf("%d.%d.%d.%d:1234", i&0xFF, (i>>8)&0xFF, (i>>16)&0xFF, (i>>24)&0xFF)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkCacheMiddleware_Miss(b *testing.B) {
	config := makeConfig(0, map[string]bool{"/cached": true})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler := cacheMiddleware(okHandler(), new(sync.Map), config)
		req := httptest.NewRequest(http.MethodGet, "/cached", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkCacheMiddleware_Hit(b *testing.B) {
	cache := new(sync.Map)
	config := makeConfig(0, map[string]bool{"/cached": true})
	handler := cacheMiddleware(okHandler(), cache, config)

	// warm up
	req := httptest.NewRequest(http.MethodGet, "/cached", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/cached", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkCacheMiddleware_NonCacheable(b *testing.B) {
	cache := new(sync.Map)
	config := makeConfig(0, map[string]bool{"/cached": true})
	handler := cacheMiddleware(okHandler(), cache, config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/other", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkCacheMiddleware_Hit_Parallel(b *testing.B) {
	cache := new(sync.Map)
	config := makeConfig(0, map[string]bool{"/cached": true})
	handler := cacheMiddleware(okHandler(), cache, config)

	req := httptest.NewRequest(http.MethodGet, "/cached", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/cached", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
		}
	})
}

func BenchmarkReloadConfig(b *testing.B) {
	path := writeTempConfig(b, map[string]any{
		"backends":       []string{"http://localhost:8080", "http://localhost:8081"},
		"cache_rules":    map[string]bool{"/": true},
		"rate_limit_max": 50,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reloadConfig(path)
	}
}

func BenchmarkFullChain_CacheHit(b *testing.B) {
	cache := new(sync.Map)
	visitors := new(sync.Map)
	config := makeConfig(1<<31, map[string]bool{"/cached": true})
	var isAlive atomic.Bool
	isAlive.Store(true)

	handler := checkHealth(rateLimit(cacheMiddleware(okHandler(), cache, config), visitors, config), &isAlive)

	req := httptest.NewRequest(http.MethodGet, "/cached", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/cached", nil)
			req.RemoteAddr = "127.0.0.1:1234"
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
		}
	})
}

func BenchmarkFullChain_CacheMiss(b *testing.B) {
	visitors := new(sync.Map)
	config := makeConfig(1<<31, map[string]bool{"/cached": true})
	var isAlive atomic.Bool
	isAlive.Store(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache := new(sync.Map)
		handler := checkHealth(rateLimit(cacheMiddleware(okHandler(), cache, config), visitors, config), &isAlive)
		req := httptest.NewRequest(http.MethodGet, "/cached", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkCacheMiddleware_ExpiredEntry(b *testing.B) {
	config := makeConfig(0, map[string]bool{"/cached": true})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache := new(sync.Map)
		cache.Store("GET:/cached", CachedResponse{
			StatusCode: http.StatusOK,
			Body:       []byte("stale"),
			Headers:    http.Header{},
			ExpiresAt:  time.Now().Add(-time.Minute),
		})
		handler := cacheMiddleware(okHandler(), cache, config)
		req := httptest.NewRequest(http.MethodGet, "/cached", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}
