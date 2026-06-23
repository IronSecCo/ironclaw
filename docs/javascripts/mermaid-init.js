// Mermaid rendering for the IronClaw docs site.
//
// We render Mermaid ourselves (pinned version, ESM import) instead of relying on
// Material's built-in integration: that integration only detects `pre.mermaid`
// and, with the `fence_code_format` output, feeds the literal `<code>` wrapper
// into the parser ("No diagram type detected for text: <code>…"), leaving an
// empty box. We emit a bare `<div class="mermaid">…` (superfences
// `fence_div_format`) — exactly what Mermaid expects natively — and drive it
// from here. GitHub renders the same ```mermaid fences natively, so README and
// docs stay in sync from one source.
//
// Node colors come from inline `classDef` directives inside each diagram (the
// steel-blue brand palette), so they are identical on GitHub and the docs site.
// The themeVariables below only style what classDef does not: edge lines and
// edge-label text, kept readable on both the light and dark (slate) schemes via
// a solid white label background.
import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@11.6.0/dist/mermaid.esm.min.mjs";

mermaid.initialize({
  startOnLoad: true,
  securityLevel: "strict",
  theme: "base",
  themeVariables: {
    fontFamily:
      "-apple-system, BlinkMacSystemFont, Segoe UI, Roboto, Helvetica, Arial, sans-serif",
    lineColor: "#64748b",
    edgeLabelBackground: "#ffffff",
  },
  flowchart: { htmlLabels: true, useMaxWidth: true },
});
