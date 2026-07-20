package scan

import (
	"fmt"
	"net/url"
	"strings"
)

// Shareable-receipt surfaces (IRO-571, IRO-576). `ironctl scan --share` turns any
// scan into a self-promoting, copy-paste artifact: a Markdown receipt with a live
// shields.io grade badge, a link to the hosted receipt card that renders the
// grade-colored per-score view, and a "Scanned with IronClaw" CTA that deep-links
// the scan coverage hub + install. Everything here is pure, deterministic, and
// offline: no network call is made at render time (the shields badge and receipt
// pages are fetched by the *viewer*, never by the CLI), so --share still emits a
// full local receipt with no connectivity.

const (
	// shareCardBaseURL is the landing site's dynamic-OG receipt route (IRO-575
	// landing PR omerzamir/nivardsec-landing#8). It reads the score from the query
	// string server-side and emits a per-score `og:image` (the grade-colored card
	// via /receipt/og), so a pasted link renders a real social preview. This is
	// the primary shared link, since only a server route can vary og:image.
	shareCardBaseURL = "https://nivardsec.com/receipt"
	// shareReceiptBaseURL is the hosted, static receipt page on our own docs
	// GitHub Pages (no board domain, no account). The score is passed in the URL
	// fragment so the page renders it client-side; the fragment is never sent to
	// a server, so the page works on plain static hosting. Kept as an offline,
	// no-dependency fallback link that renders even if the landing site is down.
	shareReceiptBaseURL = "https://ironsecco.github.io/ironclaw/receipt/"
	// scanHubURL is the scan-coverage hub the receipt CTA funnels back to.
	scanHubURL = "https://ironsecco.github.io/ironclaw/scan-coverage/"
	// installOneLiner is the copy-paste install shown in the receipt CTA. Points
	// at the canonical published installer (matches the README/quickstart).
	installOneLiner = "curl -fsSL https://raw.githubusercontent.com/IronSecCo/ironclaw/main/scripts/install.sh | sh"
)

// shareParams encodes the s/g/t/v/d receipt contract from r: score, grade,
// target, version, and the per-dimension breakdown. Both the landing OG card
// route and the static docs page decode these exact keys, so keeping one builder
// guarantees the two share URLs never drift. Built purely from the local report
// (no fetch); deterministic for a given report.
func shareParams(r Report) url.Values {
	v := url.Values{}
	v.Set("s", fmt.Sprintf("%d", r.Score))
	v.Set("g", r.Grade)
	v.Set("t", nz(r.Target, "target"))
	if r.Version != "" {
		v.Set("v", r.Version)
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
	v.Set("d", strings.Join(recs, ";"))
	return v
}

// ShareCardURL builds the primary shareable receipt link for r: the landing
// dynamic-OG route (IRO-575) with the score in the *query string* so the server
// can read it and emit a per-score, grade-colored `og:image` social card.
// Deterministic; built purely from the local report with no network call.
func ShareCardURL(r Report) string {
	// url.Values.Encode() gives a stable, sorted, percent-encoded query string.
	return shareCardBaseURL + "?" + shareParams(r).Encode()
}

// ShareReceiptURL builds the offline-fallback receipt link for r: the static docs
// GitHub Pages page with the score in the URL *fragment* (after '#') so the page
// renders the exact scorecard client-side with no server round-trip. Kept as a
// no-dependency alternate to ShareCardURL. Deterministic for a given report.
func ShareReceiptURL(r Report) string {
	// Hang the query off the fragment so it never hits the wire; the static page
	// works on plain hosting with no server to read query params.
	return shareReceiptBaseURL + "#" + shareParams(r).Encode()
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
// a grade badge (live shields.io preview) linking to the dynamic-OG receipt card,
// a headline, the per-dimension table, and a "Scanned with IronClaw" CTA that
// deep-links the scan hub + install one-liner. Pure and deterministic; makes no
// network call. The primary receipt link is the landing OG card (so social
// previews render the grade-colored per-score image); the static docs page is
// offered as an offline fallback.
func RenderShareReceipt(r Report) string {
	var b strings.Builder
	target := nz(r.Target, "target")
	card := ShareCardURL(r)

	// Badge + headline. The badge is a live shields preview; the link wraps it so
	// clicking the badge opens the dynamic-OG receipt card (per-score social image).
	fmt.Fprintf(&b, "[![IronClaw containment score](%s)](%s)\n\n", ShareBadgeURL(r), card)
	fmt.Fprintf(&b, "### IronClaw containment scan: `%s` scored **%d/100 (grade %s)**\n\n", target, r.Score, r.Grade)

	fmt.Fprintf(&b, "| Dimension | Verdict | Score |\n|---|---|---|\n")
	for _, d := range r.Dimensions {
		fmt.Fprintf(&b, "| %s | %s %s | %d/%d |\n", d.Title, mdGlyph(d.Verdict), d.Verdict, d.Score, d.Max)
	}

	fmt.Fprintf(&b, "\n[View this receipt](%s) &middot; [Static receipt](%s) &middot; [Scan your own sandbox](%s)\n\n",
		card, ShareReceiptURL(r), scanHubURL)
	fmt.Fprintf(&b, "Scanned with **IronClaw**. Grade your own container:\n\n")
	fmt.Fprintf(&b, "```sh\n%s\nironctl scan <container> --share\n```\n", installOneLiner)
	return b.String()
}

// BadgeSnippetMarkdown returns the raw one-line Markdown a user copy-pastes into
// their README to embed the live shields grade badge, wrapped in a link to the
// dynamic-OG receipt card. It reuses ShareBadgeURL and ShareCardURL, so the
// persistent README badge is byte-identical to the badge in the scan receipt and
// can never drift. Pure/offline/deterministic.
func BadgeSnippetMarkdown(r Report) string {
	return fmt.Sprintf("[![IronClaw containment score](%s)](%s)", ShareBadgeURL(r), ShareCardURL(r))
}

// RenderBadgeSnippet returns a copy-paste README-badge nudge block for r: a
// one-line "Add this to your README" CTA followed by a fenced Markdown snippet
// embedding the live grade badge (BadgeSnippetMarkdown). It turns a one-off scan
// receipt into a low-friction path to a persistent README badge -> inbound reach
// (compounds IRO-441 badge adoption). Pure/offline/deterministic; makes no
// network call, so it renders with no connectivity.
func RenderBadgeSnippet(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Add this to your README** so every visitor sees your containment grade:\n\n")
	fmt.Fprintf(&b, "```md\n%s\n```\n", BadgeSnippetMarkdown(r))
	return b.String()
}
