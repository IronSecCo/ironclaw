// This file adds Google Cloud Vertex AI as the third Google platform behind the
// same Provider/ToolConverser abstraction, alongside Google AI Studio and the
// Gemini CLI (see gemini.go). Vertex speaks the IDENTICAL Gemini wire format —
// "contents"/systemInstruction/tools out, candidates/functionCall back — so this
// file does NOT fork the translation or streaming logic: NewVertex returns a
// *GeminiProvider whose only difference is the request URL. Two things differ from
// the AI Studio path, and both live in the transport envelope rather than the body:
//
//   - URL: the GCP project and location ride in the path —
//     /v1/projects/{project}/locations/{location}/publishers/google/models/{model}:streamGenerateContent
//     served from the regional {location}-aiplatform.googleapis.com host (or the
//     global aiplatform.googleapis.com when location is "global").
//   - Auth: an OAuth2 bearer (gcloud ADC / service-account token), injected
//     host-side by modelproxy.VertexInjector — NOT the x-goog-api-key the AI Studio
//     path uses. As with every backend the sandbox holds no credential and dials
//     only the host model-proxy unix socket; the host proxy authenticates and
//     enforces the {location}-aiplatform.googleapis.com egress allowlist.

package provider

import "strings"

const (
	// defaultVertexModel matches the AI Studio default; Vertex serves the same
	// Gemini models under publishers/google/models/{model}.
	defaultVertexModel = "gemini-2.5-pro"
	// defaultVertexLocation is the region used when cfg.Location is empty. Vertex has
	// no global default region for streamGenerateContent, so we pick a common one
	// rather than emit a malformed URL.
	defaultVertexLocation = "us-central1"
	// vertexGlobalLocation selects the (region-less) global endpoint.
	vertexGlobalLocation = "global"
)

// vertexHost returns the Vertex AI host for a location: the region-less global
// endpoint for "global", otherwise the regional {location}-aiplatform.googleapis.com.
// This is the host the model-proxy must allowlist and the VertexInjector matches.
func vertexHost(location string) string {
	if location == "" || location == vertexGlobalLocation {
		return "aiplatform.googleapis.com"
	}
	return location + "-aiplatform.googleapis.com"
}

// NewVertex constructs a Vertex AI backend, reusing GeminiProvider unchanged (the
// wire format is identical) and overriding only the request URL so the GCP project
// and location ride in the path. Defaults are applied for any zero-valued field; an
// empty UpstreamHost is derived from the (possibly defaulted) location. Callers
// usually go through New. The OAuth bearer is added host-side by the model-proxy
// (modelproxy.VertexInjector) — this provider never holds a credential.
func NewVertex(cfg Config) *GeminiProvider {
	if cfg.SocketPath == "" {
		cfg.SocketPath = DefaultSocketPath
	}
	if cfg.Location == "" {
		cfg.Location = defaultVertexLocation
	}
	if cfg.UpstreamHost == "" {
		cfg.UpstreamHost = vertexHost(cfg.Location)
	}
	if cfg.Model == "" {
		cfg.Model = defaultVertexModel
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = defaultMaxTokens
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = defaultHTTPTimeout
	}

	return &GeminiProvider{
		cfg:    cfg,
		client: newSocketClient(cfg.SocketPath, cfg.HTTPTimeout),
		url:    vertexURL(cfg.UpstreamHost, cfg.Project, cfg.Location, cfg.Model),
	}
}

// vertexURL builds the Vertex AI streamGenerateContent endpoint. Like the AI Studio
// path it streams via Server-Sent Events (alt=sse) and puts the model id in the
// path; Vertex additionally threads the project and location through the path. The
// scheme is plain http to the unix socket — the host proxy upgrades to https upstream.
func vertexURL(host, project, location, model string) string {
	if location == "" {
		location = defaultVertexLocation
	}
	var b strings.Builder
	b.WriteString("http://")
	b.WriteString(host)
	b.WriteString("/v1/projects/")
	b.WriteString(project)
	b.WriteString("/locations/")
	b.WriteString(location)
	b.WriteString("/publishers/google/models/")
	b.WriteString(model)
	b.WriteString(":streamGenerateContent?alt=sse")
	return b.String()
}
