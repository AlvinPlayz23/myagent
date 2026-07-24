package main

import (
	"testing"

	"github.com/myagent/myagent/internal/auth"
	"github.com/myagent/myagent/internal/config"
	modelcatalog "github.com/myagent/myagent/internal/models"
	"github.com/myagent/myagent/internal/server/ws"
)

func TestProviderServiceSavesCustomProvidersWithoutLeakingKeys(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	dir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	store, err := auth.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	service := newProviderService(&config.Config{Providers: map[string]config.ProviderConfig{}}, store, modelcatalog.New(dir))

	list, err := service.Save(ws.ProviderInput{Name: "local", BaseURL: "http://localhost:11434/v1", Model: "qwen3", APIKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if list.DefaultModel != "local/qwen3" || len(list.Providers) != 1 {
		t.Fatalf("unexpected provider list: %#v", list)
	}
	if list.Providers[0].HasAPIKey != true || list.Providers[0].Name != "local" {
		t.Fatalf("unexpected record: %#v", list.Providers[0])
	}
	if list.Providers[0].BaseURL == "secret" {
		t.Fatal("provider record leaked API key")
	}

	if _, err := service.Delete("local"); err == nil {
		t.Fatal("expected deleting the default provider to fail")
	}
}

func TestProviderServiceSetsDefaultForConfiguredBuiltin(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	dir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	store, err := auth.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	service := newProviderService(&config.Config{Providers: map[string]config.ProviderConfig{}}, store, modelcatalog.New(dir))
	if _, err := service.Save(ws.ProviderInput{Name: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-test", APIKey: "secret", Builtin: true}); err != nil {
		t.Fatal(err)
	}
	list, err := service.SetDefault("openai", "gpt-test")
	if err != nil {
		t.Fatal(err)
	}
	if list.DefaultModel != "openai/gpt-test" {
		t.Fatalf("default = %q", list.DefaultModel)
	}
}
