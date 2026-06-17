// OWNER: T-234 (first-class persona / identity surface)

package registry

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// MaxPersonaLen caps a group's persona text. It is appended to the sandbox system
// prompt, so it must stay bounded — large enough for a meaningful persona, small
// enough that it can't crowd out the security framing or blow the context.
const MaxPersonaLen = 4096

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
