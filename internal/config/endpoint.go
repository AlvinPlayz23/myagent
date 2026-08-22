package config

import (
	"fmt"
	"strings"

	"github.com/AlvinPlayz23/myagent/internal/auth"
)

// ResolveProviderEndpoint finds the base URL and API key used to talk to a
// configured provider. Precedence is resolved per field: the base URL comes
// from the custom cfg.Providers entry first, then stored credentials in
// authStore, and finally the built-in preset as a last resort for key-less
// local servers; the API key comes from the custom config entry, then stored
// credentials (presets never supply keys). It returns an error when the
// provider has no usable endpoint anywhere.
func ResolveProviderEndpoint(cfg *Config, authStore *auth.Store, provider string) (string, string, error) {
	baseURL, apiKey := "", ""
	if cfg != nil {
		if p, ok := cfg.Providers[provider]; ok {
			baseURL, apiKey = p.BaseURL, p.APIKey
		}
	}
	if authStore != nil {
		if credentials, ok := authStore.Get(provider); ok {
			if baseURL == "" {
				baseURL = credentials.BaseURL
			}
			if apiKey == "" {
				apiKey = credentials.APIKey
			}
		}
	}
	if strings.TrimSpace(baseURL) == "" {
		if preset, ok := Preset(provider); ok {
			baseURL = preset.BaseURL
		}
	}
	if strings.TrimSpace(baseURL) == "" {
		return "", "", fmt.Errorf("provider %q has no known endpoint", provider)
	}
	return baseURL, apiKey, nil
}
