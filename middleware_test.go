package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
	var aliveBackends atomic.Pointer[[]*url.URL]
	mockURLs := []*url.URL{{Scheme: "http", Host: "localhost:8080"}}
	aliveBackends.Store(&mockURLs)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	checkHealth(okHandler(), &aliveBackends).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestCheckHealth_Dead(t *testing.T) {
	var deadBackends atomic.Pointer[[]*url.URL]
	// Zostawiamy domyślny nil - symuluje całkowity brak wczytanej puli z workerów

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	checkHealth(okHandler(), &deadBackends).ServeHTTP(rr, req)

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

func TestCacheMiddleware_NonGetOrHeadMethodsNotCached(t *testing.T) {
	methods := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			cache := new(sync.Map)
			config := makeConfig(0, map[string]bool{"/cached": true})

			called := false
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("response"))
			})

			req := httptest.NewRequest(method, "/cached", nil)
			rr := httptest.NewRecorder()
			cacheMiddleware(handler, cache, config).ServeHTTP(rr, req)

			if !called {
				t.Fatalf("expected handler to be called for %s", method)
			}
			if rr.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rr.Code)
			}
			key := method + ":/cached"
			if _, ok := cache.Load(key); ok {
				t.Fatalf("method %s should not be stored in cache", method)
			}
		})
	}
}

func TestCacheMiddleware_HeadMethodCached(t *testing.T) {
	cache := new(sync.Map)
	config := makeConfig(0, map[string]bool{"/cached": true})

	called := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodHead, "/cached", nil)
		rr := httptest.NewRecorder()
		cacheMiddleware(handler, cache, config).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rr.Code)
		}
	}

	if called != 1 {
		t.Fatalf("HEAD request should be cached on second call, backend called %d times", called)
	}
}

func TestCacheMiddleware_Non2xxStatusCodeNotCached(t *testing.T) {
	statusCodes := []int{
		http.StatusMovedPermanently,
		http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusInternalServerError,
		http.StatusBadGateway,
	}

	for _, code := range statusCodes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			cache := new(sync.Map)
			config := makeConfig(0, map[string]bool{"/cached": true})

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				w.Write([]byte("error response"))
			})

			req := httptest.NewRequest(http.MethodGet, "/cached", nil)
			rr := httptest.NewRecorder()
			cacheMiddleware(handler, cache, config).ServeHTTP(rr, req)

			if rr.Code != code {
				t.Fatalf("expected status %d, got %d", code, rr.Code)
			}
			if _, ok := cache.Load("GET:/cached"); ok {
				t.Fatalf("status %d should not be stored in cache", code)
			}
		})
	}
}

func TestCacheMiddleware_LargePayloadOver5MBNotCached(t *testing.T) {
	cache := new(sync.Map)
	config := makeConfig(0, map[string]bool{"/cached": true})

	largePayload := make([]byte, 5*1024*1024+1) // 5MB + 1 byte
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(largePayload)
	})

	req := httptest.NewRequest(http.MethodGet, "/cached", nil)
	rr := httptest.NewRecorder()
	cacheMiddleware(handler, cache, config).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if _, ok := cache.Load("GET:/cached"); ok {
		t.Fatal("response over 5MB should not be stored in cache")
	}
}

// limitClientConnections

func TestLimitClientConnections_UnderLimit(t *testing.T) {
	var inFlight atomic.Int64
	ptr := &atomic.Pointer[ProxyConfig]{}
	ptr.Store(&ProxyConfig{MaxClientConns: 5})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	limitClientConnections(okHandler(), &inFlight, ptr).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if inFlight.Load() != 0 {
		t.Fatalf("expected inFlight 0 after request, got %d", inFlight.Load())
	}
}

func TestLimitClientConnections_OverLimit(t *testing.T) {
	var inFlight atomic.Int64
	ptr := &atomic.Pointer[ProxyConfig]{}
	ptr.Store(&ProxyConfig{MaxClientConns: 1})

	// Pre-set in-flight connections to 1
	inFlight.Store(1)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	limitClientConnections(okHandler(), &inFlight, ptr).ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

// recoveryMiddleware

func TestRecoveryMiddleware_RecoversPanic(t *testing.T) {
	panickingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("simulated fatal crash in handler")
	})

	req := httptest.NewRequest(http.MethodGet, "/crash", nil)
	rr := httptest.NewRecorder()

	recoveryMiddleware(panickingHandler).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 Internal Server Error, got %d", rr.Code)
	}
}

func TestRecoveryMiddleware_NormalPassThrough(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	rr := httptest.NewRecorder()

	recoveryMiddleware(okHandler()).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}
}

