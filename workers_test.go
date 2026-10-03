package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerManager_StartAndStop(t *testing.T) {
	wm := NewWorkerManager()

	var cfg atomic.Pointer[ProxyConfig]
	cfg.Store(&ProxyConfig{Backends: []string{"http://localhost:8080"}})

	var aliveBackends atomic.Pointer[[]*url.URL]
	visitors := new(sync.Map)
	cache := new(sync.Map)

	wm.StartWorkers(
		"nonexistent.json",
		&cfg,
		&aliveBackends,
		nil,
		visitors,
		cache,
	)

	// Allow workers to start their loops
	time.Sleep(20 * time.Millisecond)

	stopped := make(chan struct{})
	go func() {
		wm.StopWorkers()
		close(stopped)
	}()

	select {
	case <-stopped:
		// Success: workers stopped cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("StopWorkers timed out; workers did not exit cleanly")
	}
}

func TestCheckHealthOnce_AliveAndDead(t *testing.T) {
	statusCode := http.StatusOK
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test" {
			w.WriteHeader(statusCode)
			w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer backend.Close()

	var cfg atomic.Pointer[ProxyConfig]
	cfg.Store(&ProxyConfig{Backends: []string{backend.URL}})

	var aliveBackends atomic.Pointer[[]*url.URL]
	client := &http.Client{Timeout: time.Second}

	// 1. Backend returning 200 OK -> should be alive
	checkHealthOnce(client, &cfg, &aliveBackends)
	alive := aliveBackends.Load()
	if alive == nil || len(*alive) != 1 {
		t.Fatalf("expected 1 alive backend, got %v", alive)
	}

	// 2. Backend returning 500 Internal Server Error -> should be marked dead
	statusCode = http.StatusInternalServerError
	checkHealthOnce(client, &cfg, &aliveBackends)
	alive = aliveBackends.Load()
	if alive == nil || len(*alive) != 0 {
		t.Fatalf("expected 0 alive backends after error, got %v", alive)
	}
}

func TestStartHealthCheck_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var cfg atomic.Pointer[ProxyConfig]
	cfg.Store(&ProxyConfig{})
	var aliveBackends atomic.Pointer[[]*url.URL]

	done := make(chan struct{})
	go func() {
		startHealthCheck(ctx, &cfg, &aliveBackends)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Passed
	case <-time.After(time.Second):
		t.Fatal("startHealthCheck did not exit on context cancellation")
	}
}

func TestStartVisitorCleaner_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	visitors := new(sync.Map)
	visitors.Store("1.2.3.4", 5)

	done := make(chan struct{})
	go func() {
		startVisitorCleaner(ctx, visitors)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Passed
	case <-time.After(time.Second):
		t.Fatal("startVisitorCleaner did not exit on context cancellation")
	}
}

func TestCacheCleaner_CleanExpiredAndCancellation(t *testing.T) {
	cache := new(sync.Map)

	// Expired entry
	cache.Store("GET:/old", CachedResponse{
		StatusCode: http.StatusOK,
		Body:       []byte("old"),
		ExpiresAt:  time.Now().Add(-time.Hour),
	})

	// Fresh entry
	cache.Store("GET:/fresh", CachedResponse{
		StatusCode: http.StatusOK,
		Body:       []byte("fresh"),
		ExpiresAt:  time.Now().Add(time.Hour),
	})

	cleanExpiredCache(cache)

	if _, ok := cache.Load("GET:/old"); ok {
		t.Fatal("expected expired entry to be deleted by cleanExpiredCache")
	}
	if _, ok := cache.Load("GET:/fresh"); !ok {
		t.Fatal("expected fresh entry to remain in cache")
	}

	// Test cancellation of startCacheCleaner
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		startCacheCleaner(ctx, cache)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Passed
	case <-time.After(time.Second):
		t.Fatal("startCacheCleaner did not exit on context cancellation")
	}
}

func TestStartConfigWatcher_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var cfg atomic.Pointer[ProxyConfig]
	cfg.Store(&ProxyConfig{})

	done := make(chan struct{})
	go func() {
		startConfigWatcher(ctx, "nonexistent.json", &cfg, nil)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Passed
	case <-time.After(time.Second):
		t.Fatal("startConfigWatcher did not exit on context cancellation")
	}
}
