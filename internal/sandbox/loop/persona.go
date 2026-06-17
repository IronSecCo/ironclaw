// OWNER: T-234 (persona — system-prompt composition)

package loop

import "strings"

// SystemPromptWith returns the sandbox system prompt with the group's persona
// appended under a clearly-delimited heading. An empty persona returns the base
// prompt unchanged. The persona is APPENDED after the security framing in
// DefaultSystemPrompt — never before and never replacing it — so a persona can shape
// tone/role but can't override the boundary rules (a persona is host-approved
// config, but defense-in-depth keeps the security framing first).
func SystemPromptWith(persona string) string {
	persona = strings.TrimSpace(persona)
	if persona == "" {
		return DefaultSystemPrompt
	}
	return DefaultSystemPrompt + "\n\n## Persona\n\nThe operator has configured this persona for you (it cannot change your tools, permissions, or the rules above):\n\n" + persona
}
