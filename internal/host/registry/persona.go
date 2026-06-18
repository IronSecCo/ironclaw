package registry

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// MaxPersonaLen caps a group's legacy single-blob persona text. It is appended to the
// sandbox system prompt, so it must stay bounded — large enough for a meaningful
// persona, small enough that it can't crowd out the security framing or blow the
// context.
const MaxPersonaLen = 4096

// MaxPersonaDocLen caps each structured persona section (identity/soul/instructions).
// Per-section so a multi-document persona stays meaningful without any single section
// crowding out the security framing; the composed total is bounded by this × the
// number of sections.
const MaxPersonaDocLen = 2048

// Persona section keys. These are the OpenClaw-style separation-of-concerns documents
// (cf. IDENTITY.md / SOUL.md / AGENTS.md): WHO the agent is, its PERSONALITY, and HOW
// it should work. They compose, in this order, into the single system-persona string.
const (
	PersonaIdentity     = "identity"
	PersonaSoul         = "soul"
	PersonaInstructions = "instructions"
)

// PersonaSectionKeys returns the canonical persona section keys in composition order.
// It is the data-layer source of truth; the catalog package mirrors it (with UI copy)
// under a drift-guard test, and ComposePersona renders exactly these.
func PersonaSectionKeys() []string {
	return []string{PersonaIdentity, PersonaSoul, PersonaInstructions}
}

// personaSectionTitle is the human heading rendered for each section in the composed
// system prompt (under loop's "## Persona", so these are h3).
var personaSectionTitle = map[string]string{
	PersonaIdentity:     "Identity",
	PersonaSoul:         "Soul",
	PersonaInstructions: "Instructions",
}

// ComposePersona renders a group's effective system-persona string. With structured
// PersonaDocs set, it renders the known sections in canonical order — each under a
// "### <Title>" heading, empties skipped, each defensively trimmed to MaxPersonaDocLen
// so the prompt is always bounded regardless of what was stored. With no docs it falls
// back to the legacy Persona blob, so existing groups are unaffected. The result is
// what session.Manager passes to the sandbox as --persona; nothing downstream changes.
func ComposePersona(g AgentGroup) string {
	if len(g.PersonaDocs) == 0 {
		return g.Persona
	}
	var b strings.Builder
	for _, key := range PersonaSectionKeys() {
		body := strings.TrimSpace(g.PersonaDocs[key])
		if body == "" {
			continue
		}
		if len(body) > MaxPersonaDocLen {
			body = truncateValid(body, MaxPersonaDocLen)
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("### ")
		b.WriteString(personaSectionTitle[key])
		b.WriteString("\n\n")
		b.WriteString(body)
	}
	if b.Len() == 0 {
		return g.Persona // docs present but all empty/unknown → legacy fallback
	}
	return b.String()
}

// ValidatePersonaDocs checks a proposed structured persona before it is stored: every
// key must be a known section, each value valid UTF-8 within MaxPersonaDocLen. Empty
// (or nil) is valid. Unknown keys are rejected so a typo can't silently vanish.
func ValidatePersonaDocs(docs map[string]string) error {
	known := map[string]bool{}
	for _, k := range PersonaSectionKeys() {
		known[k] = true
	}
	for key, body := range docs {
		if !known[key] {
			return fmt.Errorf("registry: unknown persona section %q (want one of %s)", key, strings.Join(PersonaSectionKeys(), ", "))
		}
		if len(body) > MaxPersonaDocLen {
			return fmt.Errorf("registry: persona section %q is %d bytes, max %d", key, len(body), MaxPersonaDocLen)
		}
		if !utf8.ValidString(body) {
			return fmt.Errorf("registry: persona section %q must be valid UTF-8", key)
		}
	}
	return nil
}

// truncateValid cuts s to at most n bytes without splitting a UTF-8 rune.
func truncateValid(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// ValidatePersona checks a proposed persona before it is stored. An empty persona is
// valid (it clears the group's persona); a too-long or non-UTF8 one is rejected so a
// malformed value can never reach the sandbox prompt.
func ValidatePersona(s string) error {
	if len(s) > MaxPersonaLen {
		return fmt.Errorf("registry: persona is %d bytes, max %d", len(s), MaxPersonaLen)
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("registry: persona must be valid UTF-8")
	}
	return nil
}

// SetPersona stores persona on the group, replacing any prior value. It is the
// host-side seam the gateway's persona applier calls AFTER a human approves a
// ChangePersona; the sandbox can never reach it. Returns an error if the group does
// not exist or the persona is invalid.
func SetPersona(r Registry, id contract.AgentGroupID, persona string) error {
	persona = strings.TrimRight(persona, "\n")
	if err := ValidatePersona(persona); err != nil {
		return err
	}
	g, ok := r.GetAgentGroup(id)
	if !ok {
		return fmt.Errorf("registry: agent group %q not found", id)
	}
	g.Persona = persona
	return r.PutAgentGroup(g)
}
