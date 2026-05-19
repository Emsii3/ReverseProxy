package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func makeConfig(rateLimitMax int, cacheRules map[string]bool) *atomic.Pointer[ProxyConfig] {
	ptr := &atomic.Pointer[ProxyConfig]{}
	ptr.Store(&ProxyConfig{
		RateLimitMax: rateLimitMax,
		CacheRules:   cacheRules,
	})
	return ptr
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

// checkHealth

func TestCheckHealth_Alive(t *testing.T) {
	var isAlive atomic.Bool
	isAlive.Store(true)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	checkHealth(okHandler(), &isAlive).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestCheckHealth_Dead(t *testing.T) {
	var isAlive atomic.Bool
	isAlive.Store(false)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	checkHealth(okHandler(), &isAlive).ServeHTTP(rr, req)

	if rr.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", rr.Code)
	}
}

// rateLimit

func TestRateLimit_UnderLimit(t *testing.T) {
	visitors := new(sync.Map)
	config := makeConfig(5, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rr := httptest.NewRecorder()

	rateLimit(okHandler(), visitors, config).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestRateLimit_OverLimit(t *testing.T) {
	visitors := new(sync.Map)
	config := makeConfig(2, nil)
	handler := rateLimit(okHandler(), visitors, config)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if i < 2 && rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rr.Code)
		}
		if i == 2 && rr.Code != http.StatusTooManyRequests {
			t.Fatalf("request %d: expected 429, got %d", i, rr.Code)
		}
	}
}

func TestRateLimit_DifferentIPs(t *testing.T) {
	visitors := new(sync.Map)
	config := makeConfig(1, nil)
	handler := rateLimit(okHandler(), visitors, config)

	for _, addr := range []string{"1.1.1.1:1000", "2.2.2.2:1000"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = addr
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("ip %s: expected 200, got %d", addr, rr.Code)
		}
	}
}

// cacheMiddleware

func TestCacheMiddleware_NonCacheablePath(t *testing.T) {
	cache := new(sync.Map)
	config := makeConfig(0, map[string]bool{"/cached": true})

	req := httptest.NewRequest(http.MethodGet, "/other", nil)
	rr := httptest.NewRecorder()
	cacheMiddleware(okHandler(), cache, config).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if _, ok := cache.Load("GET:/other"); ok {
		t.Fatal("non-cacheable path should not be stored")
	}
}

func TestCacheMiddleware_Miss_ThenStore(t *testing.T) {
	cache := new(sync.Map)
	config := makeConfig(0, map[string]bool{"/cached": true})

	req := httptest.NewRequest(http.MethodGet, "/cached", nil)
	rr := httptest.NewRecorder()
	cacheMiddleware(okHandler(), cache, config).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if _, ok := cache.Load("GET:/cached"); !ok {
		t.Fatal("response should be stored in cache after miss")
	}
}

func TestCacheMiddleware_Hit(t *testing.T) {
	cache := new(sync.Map)
	config := makeConfig(0, map[string]bool{"/cached": true})

	calls := 0
	counting := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	handler := cacheMiddleware(counting, cache, config)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/cached", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	if calls != 1 {
		t.Fatalf("backend should be called once, was called %d times", calls)
	}
}

func TestCacheMiddleware_ExpiredEntryDeleted(t *testing.T) {
	cache := new(sync.Map)
	config := makeConfig(0, map[string]bool{"/cached": true})

	expired := CachedResponse{
		StatusCode: http.StatusOK,
		Body:       []byte("stale"),
		Headers:    http.Header{},
		ExpiresAt:  time.Now().Add(-time.Minute),
	}
	cache.Store("GET:/cached", expired)

	req := httptest.NewRequest(http.MethodGet, "/cached", nil)
	rr := httptest.NewRecorder()
	cacheMiddleware(okHandler(), cache, config).ServeHTTP(rr, req)

	if _, ok := cache.Load("GET:/cached"); ok {
		t.Fatal("expired entry should be deleted after being served")
	}
}
