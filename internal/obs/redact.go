package obs

import (
	"log/slog"
	"strings"
)

// Masked is the placeholder substituted for a redacted value.
const Masked = "REDACTED"

// sensitiveTokens is the set of lowercased word-tokens that mark an attribute
// key as secret-bearing. Matching is token-based (see tokenize) rather than a
// naive substring test, so "session_key"/"apiKey"/"master-key" all match while
// innocuous keys such as "monkey" or "keyspace" do not.
var sensitiveTokens = map[string]struct{}{
	"key":           {},
	"keys":          {},
	"token":         {},
	"secret":        {},
	"secrets":       {},
	"password":      {},
	"passwd":        {},
	"credential":    {},
	"credentials":   {},
	"auth":          {},
	"authorization": {},
	"bearer":        {},
	"apikey":        {},
	"privatekey":    {},
	"seed":          {},
	"otp":           {},
	"pin":           {},
}

// IsSensitiveKey reports whether an attribute key names a value that must be
// masked. It splits the key into tokens on non-alphanumeric boundaries and on
// camelCase transitions, then checks each token against sensitiveTokens.
func IsSensitiveKey(key string) bool {
	for _, tok := range tokenize(key) {
		if _, ok := sensitiveTokens[tok]; ok {
			return true
		}
	}
	return false
}

// Secret returns a slog attribute whose value is already masked, for explicitly
// flagging a secret regardless of its key name (e.g. obs.Secret("payload", v)).
func Secret(key string, _ any) slog.Attr { return slog.String(key, Masked) }

// RedactString returns the masking placeholder. It exists so non-slog call
// sites (error strings, printf logs being migrated) have one canonical helper.
func RedactString(_ string) string { return Masked }

// redactingReplaceAttr is the slog.HandlerOptions.ReplaceAttr hook installed by
// New. It masks any attribute whose key is sensitive. Group nodes (Value.Kind ==
// KindGroup) are left intact so their members are still walked and redacted.
func redactingReplaceAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindGroup {
		return a
	}
	if IsSensitiveKey(a.Key) {
		return slog.String(a.Key, Masked)
	}
	return a
}

// tokenize splits s into lowercase word tokens, breaking on any non-alphanumeric
// rune and on a lower-to-upper camelCase boundary.
func tokenize(s string) []string {
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	var prev rune
	for _, r := range s {
		switch {
		case isAlphaNum(r):
			if isUpper(r) && isLower(prev) {
				flush()
			}
			cur.WriteRune(r)
		default:
			flush()
		}
		prev = r
	}
	flush()
	return tokens
}

func isAlphaNum(r rune) bool { return isLower(r) || isUpper(r) || (r >= '0' && r <= '9') }
func isLower(r rune) bool    { return r >= 'a' && r <= 'z' }
func isUpper(r rune) bool    { return r >= 'A' && r <= 'Z' }
