package main

import (
	"encoding/json"
	"os"
	"testing"
)

func writeTempConfig(t testing.TB, v any) string {
	t.Helper()
	f, err := os.CreateTemp("", "config-*.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	if err := json.NewEncoder(f).Encode(v); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

func TestReloadConfig_Valid(t *testing.T) {
	path := writeTempConfig(t, map[string]any{
		"backends":       []string{"http://localhost:8080"},
		"cache_rules":    map[string]bool{"/": true},
		"rate_limit_max": 10,
	})

	cfg := reloadConfig(path)
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}
	if len(cfg.Backends) != 1 {
		t.Fatalf("expected 1 backend, got %d", len(cfg.Backends))
	}
	if cfg.Backends[0] != "http://localhost:8080" {
		t.Fatalf("unexpected backend: %s", cfg.Backends[0])
	}
}

func TestReloadConfig_MultipleBackends(t *testing.T) {
	path := writeTempConfig(t, map[string]any{
		"backends":       []string{"http://localhost:8080", "http://localhost:8081"},
		"rate_limit_max": 5,
	})

	cfg := reloadConfig(path)
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}
	if len(cfg.Backends) != 2 {
		t.Fatalf("expected 2 backends, got %d", len(cfg.Backends))
	}
}

func TestReloadConfig_EmptyBackends(t *testing.T) {
	path := writeTempConfig(t, map[string]any{
		"backends":       []string{},
		"rate_limit_max": 5,
	})

	cfg := reloadConfig(path)
	if cfg != nil {
		t.Fatal("expected nil for empty backends")
	}
}

func TestReloadConfig_InvalidJSON(t *testing.T) {
	f, err := os.CreateTemp("", "config-*.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	f.WriteString("not valid json {{{")

	cfg := reloadConfig(f.Name())
	if cfg != nil {
		t.Fatal("expected nil for invalid JSON")
	}
}

func TestReloadConfig_MissingFile(t *testing.T) {
	cfg := reloadConfig("/nonexistent/path/config.json")
	if cfg != nil {
		t.Fatal("expected nil for missing file")
	}
}
