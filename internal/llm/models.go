package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const modelListResponseLimit = 4 << 20

// ListOpenAIModels returns the model IDs exposed by an OpenAI-compatible
// GET /models endpoint. Empty and duplicate IDs are omitted and the result is
// sorted. Discovery is deliberately separate from Provider.Stream so setup can
// use it without constructing a model first.
func ListOpenAIModels(ctx context.Context, client *http.Client, apiKey, baseURL string) ([]string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create models request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discover models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
		detail := strings.TrimSpace(string(body))
		if apiKey != "" {
			detail = strings.ReplaceAll(detail, apiKey, "[redacted]")
		}
		if detail == "" {
			detail = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("models endpoint returned %d: %s", resp.StatusCode, detail)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, modelListResponseLimit))
	if err := dec.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	seen := make(map[string]struct{}, len(payload.Data))
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	sort.Strings(models)
	return models, nil
}
