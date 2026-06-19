package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// fakeBin writes an executable shell script named cmd into a fresh dir and
// prepends that dir to PATH for the duration of the test, so exec.Command(cmd)
// resolves to our stub instead of the real binary.
func fakeBin(t *testing.T, cmd, script string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, cmd)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", cmd, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestTokenFromEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_fromenv")
	// A failing gh must never be consulted when the env var is set.
	fakeBin(t, "gh", "exit 7")
	got, err := Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got != "ghp_fromenv" {
		t.Fatalf("Token = %q, want ghp_fromenv", got)
	}
}

func TestTokenFromGHFallback(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	fakeBin(t, "gh", `echo "  ghp_fromgh  "`) // surrounding whitespace must be trimmed
	got, err := Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got != "ghp_fromgh" {
		t.Fatalf("Token = %q, want ghp_fromgh", got)
	}
}

func TestTokenMissing(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	fakeBin(t, "gh", "exit 1")
	if _, err := Token(context.Background()); err == nil {
		t.Fatal("Token: want error when env unset and gh fails, got nil")
	}
}

func TestTokenEmptyFromGH(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	fakeBin(t, "gh", "echo") // prints just a newline -> empty token
	if _, err := Token(context.Background()); err == nil {
		t.Fatal("Token: want error on empty gh token, got nil")
	}
}

func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		in          string
		owner, repo string
		ok          bool
	}{
		{"git@github.com:octocat/Hello-World.git", "octocat", "Hello-World", true},
		{"git@github.com:octocat/Hello-World", "octocat", "Hello-World", true},
		{"https://github.com/octocat/Hello-World.git", "octocat", "Hello-World", true},
		{"https://github.com/octocat/Hello-World", "octocat", "Hello-World", true},
		{"ssh://git@github.com/octocat/Hello-World.git", "octocat", "Hello-World", true},
		{"https://github.com/octocat/Hello-World.git\n", "octocat", "Hello-World", true},
		{"git@github.enterprise.io:org/team/repo.git", "team", "repo", true}, // last two segments
		{"", "", "", false},
		{"not-a-url", "", "", false},
		{"https://github.com/onlyowner", "", "", false},
	}
	for _, c := range cases {
		o, r, ok := parseRemoteURL(c.in)
		if ok != c.ok || o != c.owner || r != c.repo {
			t.Errorf("parseRemoteURL(%q) = (%q,%q,%v), want (%q,%q,%v)",
				c.in, o, r, ok, c.owner, c.repo, c.ok)
		}
	}
}

func TestRepoFromEnv(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "octocat/Hello-World")
	fakeBin(t, "git", "echo unused")
	o, r, err := Repo(context.Background())
	if err != nil || o != "octocat" || r != "Hello-World" {
		t.Fatalf("Repo = (%q,%q,%v), want (octocat,Hello-World,nil)", o, r, err)
	}
}

func TestRepoEnvMalformed(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "not-owner-repo")
	if _, _, err := Repo(context.Background()); err == nil {
		t.Fatal("Repo: want error for malformed GITHUB_REPOSITORY, got nil")
	}
}

func TestRepoFromGitFallback(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "")
	fakeBin(t, "git", `echo "git@github.com:octocat/Hello-World.git"`)
	o, r, err := Repo(context.Background())
	if err != nil || o != "octocat" || r != "Hello-World" {
		t.Fatalf("Repo = (%q,%q,%v), want (octocat,Hello-World,nil)", o, r, err)
	}
}

func TestRepoMissing(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "")
	fakeBin(t, "git", "exit 1")
	if _, _, err := Repo(context.Background()); err == nil {
		t.Fatal("Repo: want error when env unset and git fails, got nil")
	}
}

func TestAuthedTransportSetsBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hc := &http.Client{Transport: &authedTransport{token: "ghp_xyz", wrapped: http.DefaultTransport}}
	resp, err := hc.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if gotAuth != "bearer ghp_xyz" {
		t.Fatalf("Authorization = %q, want %q", gotAuth, "bearer ghp_xyz")
	}
}
