package ws

import "context"

// ProviderRecord is safe to expose to desktop clients. It deliberately never
// contains API keys or other credentials.
type ProviderRecord struct {
	Name      string   `json:"name"`
	Models    []string `json:"models"`
	Source    string   `json:"source"`
	BaseURL   string   `json:"baseUrl,omitempty"`
	HasAPIKey bool     `json:"hasApiKey"`
}

type ProviderOption struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	BaseURL string `json:"baseUrl,omitempty"`
}

type ProviderList struct {
	Providers    []ProviderRecord `json:"providers"`
	Available    []ProviderOption `json:"available"`
	DefaultModel string           `json:"defaultModel"`
}

// ProviderInput is write-only at the transport boundary. APIKey is accepted
// from a trusted local client but is never returned in a ProviderRecord.
type ProviderInput struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model"`
	APIKey  string `json:"apiKey"`
	Builtin bool   `json:"builtin"`
}

// ProviderService owns persisted provider configuration for a server process.
// Its resolver is used for new sessions so saved changes apply without a
// desktop restart; existing sessions keep their configured provider.
type ProviderService interface {
	List() (ProviderList, error)
	Save(ProviderInput) (ProviderList, error)
	Delete(name string) (ProviderList, error)
	SetDefault(name, model string) (ProviderList, error)
	Discover(ctx context.Context, name, apiKey string) ([]string, error)
}
