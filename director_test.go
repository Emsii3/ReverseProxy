package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestCustomDirector_RoundRobin(t *testing.T) {
	url1, _ := url.Parse("http://backend1:8080")
	url2, _ := url.Parse("http://backend2:8080")
	url3, _ := url.Parse("http://backend3:8080")

	backends := []*url.URL{url1, url2, url3}
	var aliveBackends atomic.Pointer[[]*url.URL]
	aliveBackends.Store(&backends)

	var counter atomic.Uint64
	director := &customDirector{
		aliveBackends:    &aliveBackends,
		backendCounter:   &counter,
		originalDirector: func(r *http.Request) {},
	}

	expectedHosts := []string{
		"backend2:8080", // 1 % 3 = 1
		"backend3:8080", // 2 % 3 = 2
		"backend1:8080", // 3 % 3 = 0
		"backend2:8080", // 4 % 3 = 1
		"backend3:8080", // 5 % 3 = 2
		"backend1:8080", // 6 % 3 = 0
	}

	for i, expectedHost := range expectedHosts {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		director.Direct(req)

		if req.URL.Host != expectedHost {
			t.Fatalf("request %d: expected URL.Host %s, got %s", i, expectedHost, req.URL.Host)
		}
		if req.Host != expectedHost {
			t.Fatalf("request %d: expected Host %s, got %s", i, expectedHost, req.Host)
		}
		if req.URL.Scheme != "http" {
			t.Fatalf("request %d: expected Scheme http, got %s", i, req.URL.Scheme)
		}
	}
}

func TestCustomDirector_Headers(t *testing.T) {
	target, _ := url.Parse("http://backend1:8080")
	backends := []*url.URL{target}
	var aliveBackends atomic.Pointer[[]*url.URL]
	aliveBackends.Store(&backends)

	var counter atomic.Uint64
	director := &customDirector{
		aliveBackends:    &aliveBackends,
		backendCounter:   &counter,
		originalDirector: func(r *http.Request) {},
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.195:45678"

	director.Direct(req)

	if xff := req.Header.Get("X-Forwarded-For"); xff != "203.0.113.195" {
		t.Fatalf("expected X-Forwarded-For '203.0.113.195', got '%s'", xff)
	}
	if proto := req.Header.Get("X-Forwarded-Proto"); proto != "http" {
		t.Fatalf("expected X-Forwarded-Proto 'http', got '%s'", proto)
	}
	if xri := req.Header.Get("X-Real-IP"); xri != "203.0.113.195" {
		t.Fatalf("expected X-Real-IP '203.0.113.195', got '%s'", xri)
	}
}

func TestCustomDirector_EmptyOrNilBackends(t *testing.T) {
	testCases := []struct {
		name     string
		backends *[]*url.URL
	}{
		{
			name:     "nil pointer to slice",
			backends: nil,
		},
		{
			name:     "empty slice",
			backends: &[]*url.URL{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var aliveBackends atomic.Pointer[[]*url.URL]
			if tc.backends != nil {
				aliveBackends.Store(tc.backends)
			}

			var counter atomic.Uint64
			originalCalled := false
			director := &customDirector{
				aliveBackends:  &aliveBackends,
				backendCounter: &counter,
				originalDirector: func(r *http.Request) {
					originalCalled = true
				},
			}

			req := httptest.NewRequest(http.MethodGet, "/path", nil)
			req.RemoteAddr = "127.0.0.1:1234"

			director.Direct(req)

			if req.Context().Err() != context.Canceled {
				t.Fatalf("expected request context to be canceled, got %v", req.Context().Err())
			}
			if originalCalled {
				t.Fatal("originalDirector should not be called when backends are empty or nil")
			}
		})
	}
}

func TestCustomDirector_OriginalDirectorCalled(t *testing.T) {
	target, _ := url.Parse("http://backend1:8080")
	backends := []*url.URL{target}
	var aliveBackends atomic.Pointer[[]*url.URL]
	aliveBackends.Store(&backends)

	var counter atomic.Uint64
	called := false
	director := &customDirector{
		aliveBackends:  &aliveBackends,
		backendCounter: &counter,
		originalDirector: func(r *http.Request) {
			called = true
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/path", nil)
	req.RemoteAddr = "127.0.0.1:1234"

	director.Direct(req)

	if !called {
		t.Fatal("expected originalDirector to be called")
	}
}

func TestCustomDirector_InvalidRemoteAddrDoesNotPanic(t *testing.T) {
	target, _ := url.Parse("http://backend1:8080")
	backends := []*url.URL{target}
	var aliveBackends atomic.Pointer[[]*url.URL]
	aliveBackends.Store(&backends)

	var counter atomic.Uint64
	director := &customDirector{
		aliveBackends:    &aliveBackends,
		backendCounter:   &counter,
		originalDirector: func(r *http.Request) {},
	}

	req := httptest.NewRequest(http.MethodGet, "/path", nil)
	req.RemoteAddr = "invalid-address-without-port"

	director.Direct(req)

	if req.URL.Host != "backend1:8080" {
		t.Fatalf("expected routing to continue despite invalid RemoteAddr, got host %s", req.URL.Host)
	}
}
