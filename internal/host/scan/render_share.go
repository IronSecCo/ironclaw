package scan

import (
	"fmt"
	"net/url"
	"strings"
)

// Shareable-receipt surfaces (IRO-571). `ironctl scan --share` turns any scan
// into a self-promoting, copy-paste artifact: a Markdown receipt with a live
// shields.io grade badge, a link to a hosted receipt page that renders the score
// client-side, and a "Scanned with IronClaw" CTA that deep-links the scan
// coverage hub + install. Everything here is pure, deterministic, and offline:
// no network call is made at render time (the shields badge and receipt page are
// fetched by the *viewer*, never by the CLI), so --share still emits a full local
// receipt with no connectivity.

const (
	// shareReceiptBaseURL is the hosted, static receipt page on our own docs
	// GitHub Pages (no board domain, no account). The score is passed in the URL
	// fragment so the page renders it client-side; the fragment is never sent to
	// a server, so the page works on plain static hosting.
	shareReceiptBaseURL = "https://ironsecco.github.io/ironclaw/receipt/"
	// scanHubURL is the scan-coverage hub the receipt CTA funnels back to.
	scanHubURL = "https://ironsecco.github.io/ironclaw/scan-coverage/"
	// installOneLiner is the copy-paste install shown in the receipt CTA. Points
	// at the canonical published installer (matches the README/quickstart).
	installOneLiner = "curl -fsSL https://raw.githubusercontent.com/IronSecCo/ironclaw/main/scripts/install.sh | sh"
)

// ShareReceiptURL builds the shareable receipt link for r. The score, grade,
// target, version, and per-dimension breakdown are encoded in the URL fragment
// (after '#') so the static receipt page can render the exact scorecard with no
// server round-trip. Deterministic for a given report.
func ShareReceiptURL(r Report) string {
	frag := url.Values{}
	frag.Set("s", fmt.Sprintf("%d", r.Score))
	frag.Set("g", r.Grade)
	frag.Set("t", nz(r.Target, "target"))
	if r.Version != "" {
		frag.Set("v", r.Version)
	}
	// Dimensions: title|verdict|score|max, records joined by ';'. Encoded as one
	// value so the page can rebuild the full per-dimension table without knowing
	// the scorer's fixed order (the dimension set differs across scan modes).
	recs := make([]string, 0, len(r.Dimensions))
	for _, d := range r.Dimensions {
		recs = append(recs, strings.Join([]string{
			d.Title, string(d.Verdict), fmt.Sprintf("%d", d.Score), fmt.Sprintf("%d", d.Max),
		}, "|"))
	}
	frag.Set("d", strings.Join(recs, ";"))
	// url.Values.Encode() gives a stable, sorted, percent-encoded query string;
	// hang it off the fragment so it never hits the wire.
	return shareReceiptBaseURL + "#" + frag.Encode()
}

// shieldsBadgeURL builds a self-contained shields.io static badge URL. No badge
// JSON needs hosting: the score is baked into the path, so the badge previews
// live anywhere the link is pasted. Follows shields' path-escaping rules
// (`-`->`--`, ` `->`_`, `/`->`%2F`).
func shieldsBadgeURL(label, message, colorHex string) string {
	esc := func(s string) string {
		s = strings.ReplaceAll(s, "-", "--")
		s = strings.ReplaceAll(s, "_", "__")
		s = strings.ReplaceAll(s, " ", "_")
		s = strings.ReplaceAll(s, "/", "%2F")
		return s
	}
	return fmt.Sprintf("https://img.shields.io/badge/%s-%s-%s",
		esc(label), esc(message), strings.TrimPrefix(colorHex, "#"))
}

// ShareBadgeURL returns the live shields grade badge URL for r.
func ShareBadgeURL(r Report) string {
	return shieldsBadgeURL("containment", fmt.Sprintf("%d/100 %s", r.Score, r.Grade), gradeColor(r.Grade))
}

// RenderShareReceipt returns a self-contained, copy-paste Markdown receipt for r:
// a grade badge (live shields.io preview), a headline, the per-dimension table,
// a link to the hosted receipt page (score encoded in the fragment), and a
// "Scanned with IronClaw" CTA that deep-links the scan hub + install one-liner.
// Pure and deterministic; makes no network call.
func RenderShareReceipt(r Report) string {
	var b strings.Builder
	target := nz(r.Target, "target")
	receipt := ShareReceiptURL(r)

	// Badge + headline. The badge is a live shields preview; the link wraps it so
	// clicking the badge opens the full receipt page.
	fmt.Fprintf(&b, "[![IronClaw containment score](%s)](%s)\n\n", ShareBadgeURL(r), receipt)
	fmt.Fprintf(&b, "### IronClaw containment scan: `%s` scored **%d/100 (grade %s)**\n\n", target, r.Score, r.Grade)

	fmt.Fprintf(&b, "| Dimension | Verdict | Score |\n|---|---|---|\n")
	for _, d := range r.Dimensions {
		fmt.Fprintf(&b, "| %s | %s %s | %d/%d |\n", d.Title, mdGlyph(d.Verdict), d.Verdict, d.Score, d.Max)
	}

	fmt.Fprintf(&b, "\n[View this receipt](%s) &middot; [Scan your own sandbox](%s)\n\n",
		receipt, scanHubURL)
	fmt.Fprintf(&b, "Scanned with **IronClaw**. Grade your own container:\n\n")
	fmt.Fprintf(&b, "```sh\n%s\nironctl scan <container> --share\n```\n", installOneLiner)
	return b.String()
}
