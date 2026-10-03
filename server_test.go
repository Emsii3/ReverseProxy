package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewProxyApp_Configuration(t *testing.T) {
	cfg := &ProxyConfig{
		Backends:            []string{"http://localhost:8080"},
		MaxConnsPerHost:     150,
		MaxIdleConnsPerHost: 250,
		MaxClientConns:      500,
	}

	app := NewProxyApp(cfg)

	if app.Server == nil {
		t.Fatal("expected Server to be non-nil")
	}
	if app.Server.Addr != ":8081" {
		t.Fatalf("expected Addr :8081, got %s", app.Server.Addr)
	}
	if app.Server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("expected ReadHeaderTimeout 5s, got %v", app.Server.ReadHeaderTimeout)
	}
	if app.Server.ReadTimeout != 15*time.Second {
		t.Fatalf("expected ReadTimeout 15s, got %v", app.Server.ReadTimeout)
	}
	if app.Server.WriteTimeout != 15*time.Second {
		t.Fatalf("expected WriteTimeout 15s, got %v", app.Server.WriteTimeout)
	}
	if app.Server.IdleTimeout != 60*time.Second {
		t.Fatalf("expected IdleTimeout 60s, got %v", app.Server.IdleTimeout)
	}
	if app.Handler == nil {
		t.Fatal("expected Handler to be non-nil")
	}
	if app.Server.Handler != app.Handler {
		t.Fatal("expected Server.Handler to match app.Handler")
	}
	if app.DynamicTransport == nil {
		t.Fatal("expected DynamicTransport to be non-nil")
	}
}

func TestDynamicTransport_RoundTrip(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("transport-ok"))
	}))
	defer backend.Close()

	cfg := &ProxyConfig{
		MaxConnsPerHost:     10,
		MaxIdleConnsPerHost: 10,
	}

	transport := &DynamicTransport{}
	transport.current.Store(createTransport(cfg))

	client := &http.Client{Transport: transport}
	resp, err := client.Get(backend.URL)
	if err != nil {
		t.Fatalf("failed to make request via DynamicTransport: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if string(body) != "transport-ok" {
		t.Fatalf("expected 'transport-ok', got '%s'", string(body))
	}
}

func TestProxyApp_EndToEnd_ForwardingAndCaching(t *testing.T) {
	backendCalls := int64(0)
	var lastForwardedFor, lastRealIP, lastForwardedProto string

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&backendCalls, 1)
		lastForwardedFor = r.Header.Get("X-Forwarded-For")
		lastRealIP = r.Header.Get("X-Real-IP")
		lastForwardedProto = r.Header.Get("X-Forwarded-Proto")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend payload"))
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("failed to parse backend URL: %v", err)
	}

	cfg := &ProxyConfig{
		Backends:            []string{backend.URL},
		RateLimitMax:        100,
		MaxClientConns:      100,
		MaxConnsPerHost:     10,
		MaxIdleConnsPerHost: 10,
		CacheRules: map[string]bool{
			"/cached": true,
		},
	}

	app := NewProxyApp(cfg)
	urls := []*url.URL{backendURL}
	app.AliveBackends.Store(&urls)

	// 1. First request to /cached -> should miss cache and hit backend
	req1 := httptest.NewRequest(http.MethodGet, "/cached", nil)
	req1.RemoteAddr = "10.0.0.1:12345"
	rr1 := httptest.NewRecorder()
	app.Handler.ServeHTTP(rr1, req1)

	if rr1.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr1.Code)
	}
	if rr1.Body.String() != "backend payload" {
		t.Fatalf("expected body 'backend payload', got '%s'", rr1.Body.String())
	}
	if atomic.LoadInt64(&backendCalls) != 1 {
		t.Fatalf("expected 1 backend call, got %d", atomic.LoadInt64(&backendCalls))
	}
	if !strings.Contains(lastForwardedFor, "10.0.0.1") {
		t.Fatalf("expected X-Forwarded-For to contain '10.0.0.1', got '%s'", lastForwardedFor)
	}
	if lastRealIP != "10.0.0.1" {
		t.Fatalf("expected X-Real-IP '10.0.0.1', got '%s'", lastRealIP)
	}
	if lastForwardedProto != "http" {
		t.Fatalf("expected X-Forwarded-Proto 'http', got '%s'", lastForwardedProto)
	}

	// 2. Second request to /cached -> should hit cache, NOT backend
	req2 := httptest.NewRequest(http.MethodGet, "/cached", nil)
	req2.RemoteAddr = "10.0.0.2:12345"
	rr2 := httptest.NewRecorder()
	app.Handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr2.Code)
	}
	if rr2.Body.String() != "backend payload" {
		t.Fatalf("expected cached body 'backend payload', got '%s'", rr2.Body.String())
	}
	if atomic.LoadInt64(&backendCalls) != 1 {
		t.Fatalf("backend should NOT be called on cache hit, got %d calls", atomic.LoadInt64(&backendCalls))
	}

	// 3. Request to /uncached -> should forward to backend
	req3 := httptest.NewRequest(http.MethodGet, "/uncached", nil)
	req3.RemoteAddr = "10.0.0.3:12345"
	rr3 := httptest.NewRecorder()
	app.Handler.ServeHTTP(rr3, req3)

	if rr3.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr3.Code)
	}
	if atomic.LoadInt64(&backendCalls) != 2 {
		t.Fatalf("expected 2 backend calls after uncached request, got %d", atomic.LoadInt64(&backendCalls))
	}
}

func TestProxyApp_EndToEnd_NoAliveBackends(t *testing.T) {
	cfg := &ProxyConfig{
		RateLimitMax:   100,
		MaxClientConns: 100,
	}

	app := NewProxyApp(cfg)
	// AliveBackends left nil

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rr := httptest.NewRecorder()
	app.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected status 504 Gateway Timeout, got %d", rr.Code)
	}
}

func TestProxyApp_EndToEnd_ConnectionLimit(t *testing.T) {
	cfg := &ProxyConfig{
		MaxClientConns: 1,
	}

	app := NewProxyApp(cfg)
	// Simulate already at max connections
	app.InFlight.Store(1)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rr := httptest.NewRecorder()
	app.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503 Service Unavailable, got %d", rr.Code)
	}
}

func TestProxyErrorHandler(t *testing.T) {
	testCases := []struct {
		name         string
		err          error
		expectedCode int
	}{
		{
			name:         "deadline exceeded",
			err:          context.DeadlineExceeded,
			expectedCode: http.StatusGatewayTimeout,
		},
		{
			name:         "context canceled",
			err:          context.Canceled,
			expectedCode: http.StatusBadGateway,
		},
		{
			name:         "upstream connection error",
			err:          errors.New("dial tcp: connection refused"),
			expectedCode: http.StatusBadGateway,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rr := httptest.NewRecorder()

			proxyErrorHandler(rr, req, tc.err)

			if rr.Code != tc.expectedCode {
				t.Fatalf("expected status %d, got %d", tc.expectedCode, rr.Code)
			}
		})
	}
}

func TestProxyApp_StartAndStopWorkers(t *testing.T) {
	cfg := &ProxyConfig{
		Backends: []string{"http://localhost:8080"},
	}
	app := NewProxyApp(cfg)
	app.StartWorkers("nonexistent.json")

	time.Sleep(20 * time.Millisecond)
	app.StopWorkers()
}


