package config

import (
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/auth"
)

func TestResolveProviderEndpointFallsBackToAuthStoreAndPresets(t *testing.T) {
	baseURL, apiKey, err := ResolveProviderEndpoint(nil, &auth.Store{Providers: map[string]auth.Credentials{
		"openrouter": {APIKey: "sk-test", BaseURL: "https://example.invalid/api/v1"},
	}}, "openrouter")
	if err != nil {
		t.Fatal(err)
	}
	if baseURL != "https://example.invalid/api/v1" || apiKey != "sk-test" {
		t.Fatalf("resolve = %q/%q, want auth store values", baseURL, apiKey)
	}

	// A custom config entry wins over both stored credentials and presets.
	baseURL, apiKey, err = ResolveProviderEndpoint(&Config{Providers: map[string]ProviderConfig{
		"ollama": {Type: DefaultProviderType, APIKey: "sk-cfg", BaseURL: "http://localhost:9999/v1"},
	}}, &auth.Store{Providers: map[string]auth.Credentials{
		"ollama": {APIKey: "sk-stored", BaseURL: "http://localhost:11434/v1"},
	}}, "ollama")
	if err != nil {
		t.Fatal(err)
	}
	if baseURL != "http://localhost:9999/v1" || apiKey != "sk-cfg" {
		t.Fatalf("resolve = %q/%q, want config values", baseURL, apiKey)
	}

	// Stored credentials fill fields the config entry leaves empty; the
	// preset covers the still-missing base URL last.
	baseURL, apiKey, err = ResolveProviderEndpoint(&Config{Providers: map[string]ProviderConfig{
		"ollama": {Type: DefaultProviderType, Model: "llama3"},
	}}, &auth.Store{Providers: map[string]auth.Credentials{
		"ollama": {APIKey: "sk-stored"},
	}}, "ollama")
	if err != nil {
		t.Fatal(err)
	}
	if baseURL != "http://localhost:11434/v1" || apiKey != "sk-stored" {
		t.Fatalf("resolve = %q/%q, want preset URL with stored key", baseURL, apiKey)
	}

	// Unknown providers without endpoints fail cleanly.
	if _, _, err := ResolveProviderEndpoint(&Config{}, &auth.Store{Providers: map[string]auth.Credentials{}}, "mystery"); err == nil {
		t.Fatal("unknown provider resolved without error")
	}
}
