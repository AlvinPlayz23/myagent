package tui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/auth"
	"github.com/AlvinPlayz23/myagent/internal/config"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
)

func TestDiscoverProviderModelsFetchesAndCachesAcceptedResults(t *testing.T) {
	dir := t.TempDir()
	catalog := modelcatalog.New(dir)

	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"zeta"},{"id":"alpha"},{"id":"alpha"}]}`)
	}))
	defer srv.Close()

	cfg := &config.Config{Providers: map[string]config.ProviderConfig{
		"local": {Type: config.DefaultProviderType, BaseURL: srv.URL + "/v1", Model: "alpha"},
	}}
	authStore := &auth.Store{Providers: map[string]auth.Credentials{}}

	ids, err := discoverProviderModels(context.Background(), catalog, cfg, authStore, "local")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "alpha" || ids[1] != "zeta" {
		t.Fatalf("discovered = %v, want [alpha zeta]", ids)
	}

	// Only the active picker accepts a result, then caches and persists it.
	catalog.RememberDiscovery("local", ids, time.Now())
	if err := catalog.SetCustomModels("local", "local", ids); err != nil {
		t.Fatalf("persist discovered models: %v", err)
	}
	if _, ok := catalog.FindModel("local", "zeta"); !ok {
		t.Fatal("discovered model was not persisted to the catalog")
	}

	// A repeat lookup within the TTL is served from memory.
	if _, err := discoverProviderModels(context.Background(), catalog, cfg, authStore, "local"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("provider endpoint hit %d times, want 1 (TTL cache missed)", got)
	}
}

func TestModelsAliasParsesAsModelCommand(t *testing.T) {
	got, err := parseSlashCommand("/models")
	if err != nil {
		t.Fatal(err)
	}
	if got.kind != commandModel || got.arg != "" {
		t.Fatalf("/models parsed as %#v, want commandModel with no arg", got)
	}
	got, err = parseSlashCommand("/models openrouter/x/y")
	if err != nil {
		t.Fatal(err)
	}
	if got.kind != commandModel || got.arg != "openrouter/x/y" {
		t.Fatalf("/models with arg parsed as %#v", got)
	}

	// The alias stays out of /help but remains reachable through the picker.
	for _, item := range commandItems {
		if item.name == "/models" && !item.hidden {
			t.Fatal("/models should be hidden from /help")
		}
	}
}
