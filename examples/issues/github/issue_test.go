package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Khan/genqlient/graphql"
)

type gqlReq struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

// gqlServer stands up a fake GraphQL endpoint that routes by operationName and
// returns each handler's value as the response "data". It records every request
// for variable assertions.
func gqlServer(t *testing.T, routes map[string]func(vars map[string]any) any) (graphql.Client, *[]gqlReq) {
	t.Helper()
	seen := &[]gqlReq{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req gqlReq
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		*seen = append(*seen, req)
		h, ok := routes[req.OperationName]
		if !ok {
			t.Errorf("unexpected operation %q", req.OperationName)
			http.Error(w, "no route", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": h(req.Variables)})
	}))
	t.Cleanup(srv.Close)
	return graphql.NewClient(srv.URL, srv.Client()), seen
}

func commentsPage(bodies []string, hasNext bool, end string) map[string]any {
	nodes := make([]any, len(bodies))
	for i, b := range bodies {
		nodes[i] = map[string]any{"body": b}
	}
	return map[string]any{"repository": map[string]any{"issue": map[string]any{
		"comments": map[string]any{
			"nodes":    nodes,
			"pageInfo": map[string]any{"hasNextPage": hasNext, "endCursor": end},
		},
	}}}
}

func TestGetIssue(t *testing.T) {
	client, seen := gqlServer(t, map[string]func(map[string]any) any{
		"IssueByNumber": func(vars map[string]any) any {
			return map[string]any{"repository": map[string]any{"issue": map[string]any{
				"id":     "I_node",
				"number": 42,
				"title":  "crash on save",
				"body":   "panic...",
				"url":    "https://github.com/octocat/Hello-World/issues/42",
				"state":  "OPEN",
			}}}
		},
	})
	got, err := GetIssue(context.Background(), client, "octocat", "Hello-World", 42)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	want := Issue{ID: "I_node", Number: 42, Title: "crash on save", Body: "panic...",
		URL: "https://github.com/octocat/Hello-World/issues/42", State: IssueStateOpen}
	if got != want {
		t.Fatalf("GetIssue = %+v, want %+v", got, want)
	}
	v := (*seen)[0].Variables
	if v["owner"] != "octocat" || v["repo"] != "Hello-World" || int(v["number"].(float64)) != 42 {
		t.Fatalf("variables = %v, want owner/repo/number forwarded", v)
	}
}

func TestGetIssueNotFound(t *testing.T) {
	client, _ := gqlServer(t, map[string]func(map[string]any) any{
		"IssueByNumber": func(map[string]any) any {
			return map[string]any{"repository": map[string]any{"issue": nil}}
		},
	})
	if _, err := GetIssue(context.Background(), client, "octocat", "Hello-World", 999); err == nil {
		t.Fatal("GetIssue: want error for missing issue, got nil")
	}
}

func TestPostComment(t *testing.T) {
	client, seen := gqlServer(t, map[string]func(map[string]any) any{
		"AddIssueComment": func(map[string]any) any {
			return map[string]any{"addComment": map[string]any{"commentEdge": map[string]any{
				"node": map[string]any{"id": "IC_1", "url": "https://github.com/octocat/Hello-World/issues/42#issuecomment-1"},
			}}}
		},
	})
	if err := PostComment(context.Background(), client, "I_node", "hello world"); err != nil {
		t.Fatalf("PostComment: %v", err)
	}
	v := (*seen)[0].Variables
	if v["subjectId"] != "I_node" || v["body"] != "hello world" {
		t.Fatalf("variables = %v, want subjectId/body forwarded", v)
	}
}

func TestCommentExistsPaginatesAndFinds(t *testing.T) {
	marker := "<!-- issues-bot:42 -->"
	client, seen := gqlServer(t, map[string]func(map[string]any) any{
		"IssueComments": func(vars map[string]any) any {
			after, _ := vars["after"].(string)
			if after == "" { // first page: marker absent, more pages
				return commentsPage([]string{"first reply", "second reply"}, true, "CURSOR1")
			}
			// second page carries the marker
			return commentsPage([]string{marker + "\nprevious summary"}, false, "")
		},
	})
	found, err := CommentExists(context.Background(), client, "octocat", "Hello-World", 42, marker)
	if err != nil {
		t.Fatalf("CommentExists: %v", err)
	}
	if !found {
		t.Fatal("CommentExists = false, want true (marker on page 2)")
	}
	if len(*seen) != 2 {
		t.Fatalf("made %d requests, want 2 (pagination)", len(*seen))
	}
	if (*seen)[1].Variables["after"] != "CURSOR1" {
		t.Fatalf("page 2 after = %v, want CURSOR1", (*seen)[1].Variables["after"])
	}
}

func TestCommentExistsAbsent(t *testing.T) {
	client, _ := gqlServer(t, map[string]func(map[string]any) any{
		"IssueComments": func(map[string]any) any {
			return commentsPage([]string{"unrelated chatter"}, false, "")
		},
	})
	found, err := CommentExists(context.Background(), client, "octocat", "Hello-World", 42, "<!-- issues-bot:42 -->")
	if err != nil {
		t.Fatalf("CommentExists: %v", err)
	}
	if found {
		t.Fatal("CommentExists = true, want false (no marker)")
	}
}
