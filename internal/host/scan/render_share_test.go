package scan

import (
	"net/url"
	"strings"
	"testing"
)

func TestShareReceiptURL(t *testing.T) {
	r := sampleReport()
	r.Version = "v0.1.361"
	u := ShareReceiptURL(r)

	if !strings.HasPrefix(u, shareReceiptBaseURL+"#") {
		t.Fatalf("receipt URL must hang the score off the fragment: %q", u)
	}
	// Parse the fragment as a query string (that is how the page decodes it).
	frag := strings.SplitN(u, "#", 2)[1]
	vals, err := url.ParseQuery(frag)
	if err != nil {
		t.Fatalf("fragment is not decodable: %v", err)
	}
	if vals.Get("s") != "100" {
		t.Errorf("score param = %q, want 100", vals.Get("s"))
	}
	if vals.Get("g") != "A" {
		t.Errorf("grade param = %q, want A", vals.Get("g"))
	}
	if vals.Get("t") != "ic-sbx-demo" {
		t.Errorf("target param = %q", vals.Get("t"))
	}
	if vals.Get("v") != "v0.1.361" {
		t.Errorf("version param = %q", vals.Get("v"))
	}
	// The dimension breakdown round-trips: one record per graded dimension.
	recs := strings.Split(vals.Get("d"), ";")
	if len(recs) != len(r.Dimensions) {
		t.Fatalf("dims encoded %d, want %d", len(recs), len(r.Dimensions))
	}
	first := strings.Split(recs[0], "|")
	if len(first) != 4 {
		t.Fatalf("dim record must be title|verdict|score|max, got %q", recs[0])
	}
	if first[0] != r.Dimensions[0].Title {
		t.Errorf("first dim title = %q, want %q", first[0], r.Dimensions[0].Title)
	}

	// Determinism: same report renders the same URL.
	if ShareReceiptURL(r) != u {
		t.Error("ShareReceiptURL is not deterministic")
	}
}

func TestShareBadgeURL(t *testing.T) {
	u := ShareBadgeURL(sampleReport())
	// Well-formed absolute shields.io badge URL.
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("badge URL invalid: %v", err)
	}
	if parsed.Host != "img.shields.io" {
		t.Errorf("badge host = %q, want img.shields.io", parsed.Host)
	}
	// Score baked into the path (no hosted JSON needed): '/' escapes to %2F.
	if !strings.Contains(u, "100%2F100_A") {
		t.Errorf("badge URL missing baked score: %q", u)
	}
	// Grade color present, bare hex (no '#').
	if !strings.HasSuffix(u, strings.TrimPrefix(gradeColor("A"), "#")) {
		t.Errorf("badge URL missing grade color: %q", u)
	}

	// A failing report renders red, not green.
	if !strings.HasSuffix(ShareBadgeURL(Score(Spec{})), strings.TrimPrefix(gradeColor("F"), "#")) {
		t.Error("failing badge is not red")
	}
}

func TestRenderShareReceipt(t *testing.T) {
	md := RenderShareReceipt(sampleReport())
	for _, want := range []string{
		"img.shields.io",                        // live badge preview
		"ironsecco.github.io/ironclaw/receipt/", // hosted receipt page
		"scan-coverage",                         // funnel back to the hub
		"| Dimension |",                         // per-dim breakdown
		"Scanned with **IronClaw**",             // CTA
		"ironctl scan <container> --share",
		"install.sh", // install one-liner
	} {
		if !strings.Contains(md, want) {
			t.Errorf("share receipt missing %q\n%s", want, md)
		}
	}
	// Public-copy house style: no em/en-dashes (IRO-254).
	if strings.ContainsAny(md, "—–") {
		t.Error("share receipt contains an em/en-dash")
	}
	// Fail-safe/offline: rendering must not require or perform any network I/O.
	// (Pure string building — asserted implicitly by this test running offline.)
	if RenderShareReceipt(sampleReport()) != md {
		t.Error("RenderShareReceipt is not deterministic")
	}
}
