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

func TestReloadConfig_LimitsAndDefaults(t *testing.T) {
	// Test defaults when values are not specified (0)
	pathDefault := writeTempConfig(t, map[string]any{
		"backends": []string{"http://localhost:8080"},
	})
	cfgDefault := reloadConfig(pathDefault)
	if cfgDefault == nil {
		t.Fatal("expected config, got nil")
	}
	if cfgDefault.MaxClientConns != 100 {
		t.Fatalf("expected default MaxClientConns 100, got %d", cfgDefault.MaxClientConns)
	}
	if cfgDefault.MaxConnsPerHost != 100 {
		t.Fatalf("expected default MaxConnsPerHost 100, got %d", cfgDefault.MaxConnsPerHost)
	}
	if cfgDefault.MaxIdleConnsPerHost != 500 {
		t.Fatalf("expected default MaxIdleConnsPerHost 500, got %d", cfgDefault.MaxIdleConnsPerHost)
	}

	// Test custom values
	pathCustom := writeTempConfig(t, map[string]any{
		"backends":                []string{"http://localhost:8080"},
		"max_conns_per_host":     150,
		"max_idle_conns_per_host": 250,
		"max_client_conns":        450,
	})
	cfgCustom := reloadConfig(pathCustom)
	if cfgCustom == nil {
		t.Fatal("expected config, got nil")
	}
	if cfgCustom.MaxConnsPerHost != 150 {
		t.Fatalf("expected MaxConnsPerHost 150, got %d", cfgCustom.MaxConnsPerHost)
	}
	if cfgCustom.MaxIdleConnsPerHost != 250 {
		t.Fatalf("expected MaxIdleConnsPerHost 250, got %d", cfgCustom.MaxIdleConnsPerHost)
	}
	if cfgCustom.MaxClientConns != 450 {
		t.Fatalf("expected MaxClientConns 450, got %d", cfgCustom.MaxClientConns)
	}
}
