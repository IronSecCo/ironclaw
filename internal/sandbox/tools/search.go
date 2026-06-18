package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// WebSearchToolName is the tool the agent calls to run a web search through the host
// egress broker. Like http_fetch it has no network of its own: it dials only the
// bound egress socket, and the host broker forwards the call to the operator-approved
// search backend (every call audited host-side). A non-approved backend host comes
// back as 403.
const WebSearchToolName = "web_search"

// Bounds for a single search so one call cannot blow up the turn.
const (
	defaultSearchResults = 5
	maxSearchResults     = 10
	maxSearchBodyBytes   = 512 * 1024
	searchTimeout        = 30 * time.Second
	maxSnippetChars      = 300
)

// vaultHost mirrors the egress broker's reserved vault host (internal/host/egress.
// VaultHost). It is duplicated here as a small wire constant rather than imported so
// the sandbox tools never depend on host-side packages (the layering boundary).
const vaultHost = "vault"

// WebSearchTool runs a query against an operator-configured search backend over the
// egress broker socket. The BACKEND is chosen host-side (a launch flag), never by the
// agent: the agent only supplies a query string, so it can neither pick an arbitrary
// provider nor reach a host the operator has not approved. Two backend families are
// supported (see ParseSearchBackend):
//   - DuckDuckGo's keyless Instant Answer API — no credential, the zero-secrets
//     default, but it returns instant answers / related topics rather than a full
//     ranked web index, so specific lookups can come back thin.
//   - A keyed provider reached by NAME through the credential vault (vault://<cred>/…),
//     so the API key lives host-side in the injector and never enters the sandbox.
type WebSearchTool struct {
	client  *http.Client
	backend searchBackend
}

// NewWebSearchTool builds the tool over the egress broker socket bound into the
// sandbox, for the given backend spec. The HTTP client dials ONLY that unix socket
// regardless of the target host, so every request necessarily traverses the host
// broker. An unrecognised backend spec is a construction error.
func NewWebSearchTool(socketPath, backendSpec string) (*WebSearchTool, error) {
	backend, err := ParseSearchBackend(backendSpec)
	if err != nil {
		return nil, err
	}
	return &WebSearchTool{
		client: &http.Client{
			Timeout: searchTimeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
				},
			},
		},
		backend: backend,
	}, nil
}

// Name identifies the tool.
func (t *WebSearchTool) Name() string { return WebSearchToolName }

// Description frames the tool for the model in-band.
func (t *WebSearchTool) Description() string {
	return "Search the web and return a short list of results (title, URL, snippet). " +
		"Queries are brokered and audited by the host and reach the operator-approved " +
		"search backend only. Use it to look up facts you do not already know, recent " +
		"events, or to find information about a person, place, or topic online."
}

// JSONSchema returns the input schema.
func (t *WebSearchTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"query":{"type":"string","description":"The search query."},` +
		`"max_results":{"type":"integer","description":"Maximum results to return (1-10, default 5).","minimum":1,"maximum":10}` +
		`},"required":["query"],"additionalProperties":false}`)
}

type webSearchInput struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
}

// Invoke runs the search: it builds the backend's brokered request, sends it over the
// egress socket, and renders the parsed results for the model. A broker denial (host
// not allowlisted, 403) or a thin/empty result set is returned as ordinary tool
// content so the model can explain it; only a transport failure is a tool error.
func (t *WebSearchTool) Invoke(ctx context.Context, input json.RawMessage) (string, error) {
	var in webSearchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("web_search: invalid input: %w", err)
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return "", fmt.Errorf("web_search: query is required")
	}
	n := in.MaxResults
	if n <= 0 {
		n = defaultSearchResults
	}
	if n > maxSearchResults {
		n = maxSearchResults
	}

	host, path, q := t.backend.request(query, n)
	reqURL := &url.URL{Scheme: "http", Host: host, Path: path, RawQuery: q.Encode()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("web_search: build request: %w", err)
	}
	req.Host = host
	// A real User-Agent + broad Accept: the keyless DuckDuckGo HTML endpoint returns
	// an empty/blocked page to UA-less clients; JSON backends (Brave via vault) ignore
	// both. Set here (not per-backend) since every search rides the same client.
	req.Header.Set("Accept", "text/html,application/json;q=0.9,*/*;q=0.8")
	req.Header.Set("User-Agent", searchUserAgent)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_search: request failed (egress broker unreachable or upstream error): %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSearchBodyBytes))
	if err != nil {
		return "", fmt.Errorf("web_search: read response: %w", err)
	}

	if resp.StatusCode == http.StatusForbidden {
		// The broker rejects a host not on the operator allowlist with 403. Surface it
		// as guidance, not a crash, so the agent can explain the approval gap.
		return fmt.Sprintf("web_search: the %s search backend is not on this agent's egress allowlist (403). "+
			"An operator must approve it through the control-plane before web search works here.", t.backend.label()), nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Sprintf("web_search: the %s backend returned HTTP %d. The query was %q.",
			t.backend.label(), resp.StatusCode, query), nil
	}

	results, err := t.backend.parse(data)
	if err != nil {
		return "", fmt.Errorf("web_search: %w", err)
	}
	return renderSearchResults(t.backend.label(), query, dedupeResults(results), n), nil
}

// searchResult is one normalized hit returned to the model.
type searchResult struct {
	Title   string
	URL     string
	Snippet string
}

// searchBackend builds the brokered request for a query and parses the provider's
// response. Implementations are selected host-side; the agent never picks one.
type searchBackend interface {
	// request returns the socket-hop request: the Host the broker reads to select the
	// upstream (a real host, or the reserved "vault" host for a keyed provider), the
	// path, and the URL query values.
	request(query string, maxResults int) (host, path string, q url.Values)
	// parse turns the provider's response body into normalized results.
	parse(body []byte) ([]searchResult, error)
	// label names the backend for diagnostics and the no-results / 403 hints.
	label() string
	// egressHost is the external host an operator must allowlist for this backend, or
	// "" when it is reached through the vault (the injector endpoint is allowlisted via
	// --vault-endpoint instead). Used host-side to auto-approve the backend's host.
	egressHost() string
}

// ParseSearchBackend resolves a backend spec into a searchBackend. Recognised specs:
//
//	"duckduckgo" / "ddg"  -> keyless DuckDuckGo Instant Answer API
//	"brave"               -> Brave Search via vault credential "brave"
//	"brave:<cred>"        -> Brave Search via vault credential <cred>
//
// The keyed forms require the egress broker's vault to be configured (--vault-endpoint)
// with a matching credential; the sandbox never holds the key.
func ParseSearchBackend(spec string) (searchBackend, error) {
	name, arg, _ := strings.Cut(strings.ToLower(strings.TrimSpace(spec)), ":")
	switch name {
	case "duckduckgo", "ddg":
		return duckDuckGoBackend{}, nil
	case "brave":
		cred := strings.TrimSpace(arg)
		if cred == "" {
			cred = "brave"
		}
		return vaultSearchBackend{cred: cred, provider: braveProvider{}}, nil
	case "":
		return nil, fmt.Errorf("web_search: empty search backend spec")
	default:
		return nil, fmt.Errorf("web_search: unknown search backend %q (want duckduckgo or brave[:cred])", spec)
	}
}

// SearchBackendEgressHost returns the external host an operator must allowlist on the
// egress broker for the given search backend spec, or "" for a vault-routed backend
// (whose injector endpoint is allowlisted via --vault-endpoint). It lets the host-side
// daemon auto-approve a configured backend's host so the tool is not present-but-403.
func SearchBackendEgressHost(spec string) (string, error) {
	b, err := ParseSearchBackend(spec)
	if err != nil {
		return "", err
	}
	return b.egressHost(), nil
}

// --- DuckDuckGo (keyless) ---------------------------------------------------

// duckDuckGoHost is DuckDuckGo's keyless HTML results endpoint host. We use it
// rather than the Instant Answer API (api.duckduckgo.com): the Instant Answer API
// only returns entity abstracts / related topics, so most real queries (news, a
// person, current events) come back empty. The HTML endpoint returns the actual
// ranked web results, still with NO credential.
const duckDuckGoHost = "html.duckduckgo.com"

// searchUserAgent identifies search requests as a normal browser; the DuckDuckGo
// HTML endpoint returns an empty/blocked page to a UA-less client.
const searchUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// duckDuckGoBackend scrapes DuckDuckGo's keyless HTML results page. It needs no
// credential (the zero-secrets default) and returns real ranked web results. The
// endpoint is unofficial, so it can rate-limit; a keyed Brave provider via the vault
// is the more robust choice when an operator has a key.
type duckDuckGoBackend struct{}

func (duckDuckGoBackend) label() string      { return "duckduckgo" }
func (duckDuckGoBackend) egressHost() string { return duckDuckGoHost }

func (duckDuckGoBackend) request(query string, _ int) (string, string, url.Values) {
	q := url.Values{}
	q.Set("q", query)
	q.Set("kl", "wt-wt") // no region bias
	return duckDuckGoHost, "/html/", q
}

// DDG HTML result markup: each hit is an <a class="result__a" href="REDIRECT">TITLE</a>
// followed by an <a class="result__snippet">SNIPPET</a>. The href is a DDG redirect
// wrapper carrying the real URL in its uddg= query param.
var (
	ddgResultRe  = regexp.MustCompile(`(?s)<a[^>]+class="[^"]*result__a[^"]*"[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippetRe = regexp.MustCompile(`(?s)<a[^>]+class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>`)
	htmlTagRe    = regexp.MustCompile(`<[^>]+>`)
)

func (duckDuckGoBackend) parse(body []byte) ([]searchResult, error) {
	s := string(body)
	titles := ddgResultRe.FindAllStringSubmatch(s, -1)
	snippets := ddgSnippetRe.FindAllStringSubmatch(s, -1)
	out := make([]searchResult, 0, len(titles))
	for i, m := range titles {
		title := cleanHTMLText(m[2])
		link := ddgDecodeURL(m[1])
		if title == "" && link == "" {
			continue
		}
		snip := ""
		if i < len(snippets) {
			snip = cleanHTMLText(snippets[i][1])
		}
		out = append(out, searchResult{Title: title, URL: link, Snippet: snip})
	}
	return out, nil
}

// ddgDecodeURL turns a DDG redirect href (//duckduckgo.com/l/?uddg=<encoded>&…) into
// the real destination URL; a direct or protocol-relative href is returned as-is.
func ddgDecodeURL(href string) string {
	href = html.UnescapeString(href)
	if i := strings.Index(href, "uddg="); i >= 0 {
		v := href[i+len("uddg="):]
		if amp := strings.IndexByte(v, '&'); amp >= 0 {
			v = v[:amp]
		}
		if dec, err := url.QueryUnescape(v); err == nil && dec != "" {
			return dec
		}
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	return href
}

// cleanHTMLText strips tags and decodes HTML entities from a fragment of result markup.
func cleanHTMLText(s string) string {
	return strings.TrimSpace(html.UnescapeString(htmlTagRe.ReplaceAllString(s, "")))
}

// --- Keyed provider via the credential vault --------------------------------

// vaultSearchBackend reaches a keyed search provider by NAME through the credential
// vault: it addresses the reserved vault host with path /<cred>/<providerPath> so the
// host injector attaches the API key (the sandbox never holds it). The provider shape
// (request path + response parsing) is fixed per provider; cred is the logical vault
// credential name the operator configured.
type vaultSearchBackend struct {
	cred     string
	provider keyedProvider
}

// keyedProvider is a keyed search API's request/response shape, independent of how the
// credential is supplied (the vault injector handles that host-side).
type keyedProvider interface {
	name() string
	path() string
	query(query string, maxResults int) url.Values
	parse(body []byte) ([]searchResult, error)
}

func (v vaultSearchBackend) label() string      { return v.provider.name() + " (vault:" + v.cred + ")" }
func (v vaultSearchBackend) egressHost() string { return "" } // injector endpoint allowlisted via --vault-endpoint

func (v vaultSearchBackend) request(query string, n int) (string, string, url.Values) {
	return vaultHost, "/" + v.cred + v.provider.path(), v.provider.query(query, n)
}

func (v vaultSearchBackend) parse(body []byte) ([]searchResult, error) {
	return v.provider.parse(body)
}

// braveProvider is the Brave Search API shape: GET /res/v1/web/search?q=&count=, with
// the X-Subscription-Token attached host-side by the vault injector.
type braveProvider struct{}

func (braveProvider) name() string { return "brave" }
func (braveProvider) path() string { return "/res/v1/web/search" }

func (braveProvider) query(query string, n int) url.Values {
	q := url.Values{}
	q.Set("q", query)
	q.Set("count", fmt.Sprintf("%d", n))
	return q
}

func (braveProvider) parse(body []byte) ([]searchResult, error) {
	var r struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("decode brave response: %w", err)
	}
	out := make([]searchResult, 0, len(r.Web.Results))
	for _, h := range r.Web.Results {
		out = append(out, searchResult{Title: h.Title, URL: h.URL, Snippet: h.Description})
	}
	return out, nil
}

// --- rendering helpers ------------------------------------------------------

// dedupeResults drops repeat hits (DDG in particular repeats topics), keying on the
// URL when present and otherwise the snippet text.
func dedupeResults(in []searchResult) []searchResult {
	seen := make(map[string]struct{}, len(in))
	out := make([]searchResult, 0, len(in))
	for _, r := range in {
		key := r.URL
		if key == "" {
			key = r.Snippet
		}
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, r)
	}
	return out
}

// renderSearchResults formats up to max results for the model, with a backend-specific
// hint when the set is empty.
func renderSearchResults(label, query string, results []searchResult, max int) string {
	if len(results) == 0 {
		msg := fmt.Sprintf("No results for %q from the %s backend.", query, label)
		if label == "duckduckgo" {
			msg += " The keyless DuckDuckGo endpoint may have rate-limited or returned no hits; " +
				"retry, refine the query, or configure a keyed Brave provider via the vault for " +
				"more robust results."
		}
		return msg
	}
	if len(results) > max {
		results = results[:max]
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Search results for %q (%s):\n\n", query, label)
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, firstNonEmpty(strings.TrimSpace(r.Title), r.URL, "(untitled)"))
		if r.URL != "" {
			fmt.Fprintf(&sb, "   %s\n", r.URL)
		}
		if s := strings.TrimSpace(r.Snippet); s != "" {
			fmt.Fprintf(&sb, "   %s\n", clipSnippet(s, maxSnippetChars))
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// firstNonEmpty returns the first non-empty argument, or "" if all are empty.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// clipSnippet truncates s to at most n characters on a rune boundary, appending an
// ellipsis when it cut.
func clipSnippet(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}
