package llm

import (
	"net/http"
	"net/url"
	"strings"
)

// Client identity sent to Zen for routing/attribution. This is an honest
// client name, never a spoofed OpenCode User-Agent.
const (
	zenClientName = "myagent"
	zenUserAgent  = "myagent/0.1.0"
	zenHost       = "opencode.ai"
)

// IsMuseSparkModel reports whether a model ID targets Meta Muse Spark.
// Zen serves these models on the Responses API (/v1/responses); chat
// completions (/v1/chat/completions) returns 500 for them.
func IsMuseSparkModel(modelID string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(modelID)), "muse-spark")
}

// IsContributorTier reports whether a model ID is a Contributor/free-tier
// Muse Spark variant. Per Meta docs, "max" reasoning effort is Standard-tier
// muse-spark-1.3 only and is not available on Contributor-tier models.
func IsContributorTier(modelID string) bool {
	lower := strings.ToLower(strings.TrimSpace(modelID))
	return strings.Contains(lower, "contributor") || strings.HasSuffix(lower, "-free")
}

// ClampContributorEffort maps unsupported effort levels for Contributor-tier
// Muse Spark to the nearest supported level. Currently only "max" needs
// clamping (contributor supports up to "xhigh").
func ClampContributorEffort(modelID string, effort Effort) Effort {
	if effort == EffortMax && IsMuseSparkModel(modelID) && IsContributorTier(modelID) {
		return EffortXHigh
	}
	return effort
}

// ClassifyZenError returns an actionable hint for known Zen (/v1) failure
// shapes. It returns "" when the body does not match a known case.
// Matching is case-insensitive and deliberately substring-based because Zen
// nests the vendor message inside several envelope shapes.
func ClassifyZenError(status int, body, modelID, endpoint string) string {
	lower := strings.ToLower(body)
	isResponses := strings.HasSuffix(endpoint, "/responses")

	switch {
	case strings.Contains(lower, "missingsessionid"),
		strings.Contains(lower, "can only be used in opencode"),
		strings.Contains(lower, "freeusagelimiterror"),
		strings.Contains(lower, "free tier"):
		return " — Zen free model " + quoteModel(modelID) + " requires an OpenCode client session (x-opencode-session) and is not usable without it by vendor policy; use a paid Zen model or OpenCode"
	case strings.Contains(lower, "model is unavailable"),
		strings.Contains(lower, "not supported"),
		strings.Contains(lower, "modelerror"):
		return " — Zen reports model " + quoteModel(modelID) + " unavailable/unsupported on /v1; refresh GET /v1/models and pick another Zen model"
	case status == 500 && IsMuseSparkModel(modelID) && !isResponses:
		return " — Muse Spark is served on Zen /responses, not /chat/completions; myagent routes muse-spark* to /responses automatically"
	default:
		return ""
	}
}

func quoteModel(modelID string) string {
	if strings.TrimSpace(modelID) == "" {
		return "unknown"
	}
	return `"` + modelID + `"`
}

// IsZenHost reports whether a base URL points at the OpenCode Zen gateway.
// Session-affinity headers are only sent there, mirroring pi's host guard,
// so credentials/identifiers never leak to other providers.
func IsZenHost(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), zenHost)
}

// setZenHeaders applies Zen session-affinity headers to an outgoing request.
// It mirrors pi's provider-attribution (x-opencode-session + x-opencode-client)
// and only acts on the Zen host with a non-empty session ID.
func setZenHeaders(httpReq *http.Request, model Model) {
	if model.SessionID == "" || !IsZenHost(model.BaseURL) {
		return
	}
	if httpReq.Header.Get("x-opencode-session") == "" {
		httpReq.Header.Set("x-opencode-session", model.SessionID)
	}
	if httpReq.Header.Get("x-opencode-client") == "" {
		httpReq.Header.Set("x-opencode-client", zenClientName)
	}
	if httpReq.Header.Get("User-Agent") == "" {
		httpReq.Header.Set("User-Agent", zenUserAgent)
	}
}
