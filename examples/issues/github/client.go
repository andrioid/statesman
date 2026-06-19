package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/Khan/genqlient/graphql"
)

// Endpoint is GitHub's GraphQL API. (GitHub Enterprise is out of scope.)
const Endpoint = "https://api.github.com/graphql"

// Token resolves a GitHub API token: GITHUB_TOKEN if set, otherwise the gh
// CLI's `gh auth token`. This is the only remaining use of gh — every data
// operation goes through GraphQL. The context bounds the gh subprocess, so a
// state-exit cancellation kills it like any other attempt.
func Token(ctx context.Context) (string, error) {
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t, nil
	}
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		return "", fmt.Errorf("github: GITHUB_TOKEN unset and `gh auth token` failed: %w", err)
	}
	t := strings.TrimSpace(string(out))
	if t == "" {
		return "", errors.New("github: `gh auth token` returned an empty token")
	}
	return t, nil
}

// Repo resolves the target owner/repo: GITHUB_REPOSITORY ("owner/repo", the
// GitHub Actions convention) if set, otherwise the origin remote of the local
// git checkout. GraphQL has no "current repository" notion, so it must be
// explicit — unlike `gh`, which inferred it from the remote.
func Repo(ctx context.Context) (owner, repo string, err error) {
	if r := strings.TrimSpace(os.Getenv("GITHUB_REPOSITORY")); r != "" {
		o, n, ok := splitOwnerRepo(r)
		if !ok {
			return "", "", fmt.Errorf("github: GITHUB_REPOSITORY %q is not \"owner/repo\"", r)
		}
		return o, n, nil
	}
	out, err := exec.CommandContext(ctx, "git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", "", fmt.Errorf("github: GITHUB_REPOSITORY unset and `git remote get-url origin` failed: %w", err)
	}
	o, n, ok := parseRemoteURL(string(out))
	if !ok {
		return "", "", fmt.Errorf("github: cannot parse git remote %q", strings.TrimSpace(string(out)))
	}
	return o, n, nil
}

// splitOwnerRepo parses the exact "owner/repo" form (GITHUB_REPOSITORY).
func splitOwnerRepo(s string) (owner, repo string, ok bool) {
	o, r, found := strings.Cut(strings.TrimSpace(s), "/")
	if !found || o == "" || r == "" || strings.Contains(r, "/") {
		return "", "", false
	}
	return o, r, true
}

// parseRemoteURL extracts owner/repo from a git remote URL, accepting both
// scp-like (git@host:owner/repo[.git]) and URL (scheme://[user@]host/owner/
// repo[.git]) forms. It uses the final two path segments, so nested
// Enterprise paths resolve to the trailing owner/repo.
func parseRemoteURL(raw string) (owner, repo string, ok bool) {
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(s, ".git")
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		if at := strings.LastIndex(s, "@"); at >= 0 {
			s = s[at+1:]
		}
		slash := strings.Index(s, "/")
		if slash < 0 {
			return "", "", false
		}
		s = s[slash+1:]
	} else if at := strings.Index(s, "@"); at >= 0 {
		s = s[at+1:]
		colon := strings.Index(s, ":")
		if colon < 0 {
			return "", "", false
		}
		s = s[colon+1:]
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	owner, repo = parts[len(parts)-2], parts[len(parts)-1]
	if owner == "" || repo == "" {
		return "", "", false
	}
	return owner, repo, true
}

// authedTransport injects the bearer token on every request.
type authedTransport struct {
	token   string
	wrapped http.RoundTripper
}

func (t *authedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "bearer "+t.token)
	return t.wrapped.RoundTrip(req)
}

// NewClient builds a genqlient client authenticated with the resolved Token.
func NewClient(ctx context.Context) (graphql.Client, error) {
	tok, err := Token(ctx)
	if err != nil {
		return nil, err
	}
	hc := &http.Client{Transport: &authedTransport{token: tok, wrapped: http.DefaultTransport}}
	return graphql.NewClient(Endpoint, hc), nil
}
