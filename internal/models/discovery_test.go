package models

import (
	"testing"
	"time"
)

func TestRememberAndCachedDiscovery(t *testing.T) {
	c := New(t.TempDir())
	now := time.Now()

	if _, ok := c.CachedDiscovery("ollama", now); ok {
		t.Fatal("fresh catalog reported a cached discovery")
	}

	c.RememberDiscovery("ollama", []string{"llama3", "qwen"}, now)
	ids, ok := c.CachedDiscovery("ollama", now.Add(discoveryTTL-time.Second))
	if !ok {
		t.Fatal("cached discovery expired immediately")
	}
	if len(ids) != 2 || ids[0] != "llama3" || ids[1] != "qwen" {
		t.Fatalf("cached discovery = %v, want [llama3 qwen]", ids)
	}

	// Mutating the returned slice must not corrupt the cache.
	ids[0] = "mutated"
	ids, _ = c.CachedDiscovery("ollama", now)
	if ids[0] != "llama3" {
		t.Fatalf("cache leaked mutations: %v", ids)
	}
}

func TestCachedDiscoveryExpiresAfterTTL(t *testing.T) {
	c := New(t.TempDir())
	now := time.Now()

	c.RememberDiscovery("openrouter", []string{"vendor/model"}, now)
	if _, ok := c.CachedDiscovery("openrouter", now.Add(discoveryTTL)); ok {
		t.Fatal("discovery still cached exactly at TTL")
	}
	if _, ok := c.CachedDiscovery("other", now); ok {
		t.Fatal("unrelated provider shared another provider's cache entry")
	}

	// A fresh lookup overwrites the stale entry.
	c.RememberDiscovery("openrouter", []string{"vendor/model-v2"}, now.Add(discoveryTTL+time.Minute))
	ids, ok := c.CachedDiscovery("openrouter", now.Add(discoveryTTL+time.Minute))
	if !ok || len(ids) != 1 || ids[0] != "vendor/model-v2" {
		t.Fatalf("refreshed cache = %v, ok = %v", ids, ok)
	}
}
