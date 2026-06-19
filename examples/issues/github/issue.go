package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khan/genqlient/graphql"
)

// Issue is a flattened view of the issue fields the triage pipeline needs,
// hiding genqlient's deeply-nested per-selection response types.
type Issue struct {
	ID     string
	Number int
	Title  string
	Body   string
	URL    string
	State  IssueState
}

// GetIssue fetches one issue by number in a single round-trip. A missing issue
// (null repository.issue) is reported as an error rather than a zero Issue.
func GetIssue(ctx context.Context, client graphql.Client, owner, repo string, number int) (Issue, error) {
	resp, err := IssueByNumber(ctx, client, owner, repo, number)
	if err != nil {
		return Issue{}, err
	}
	i := resp.Repository.Issue
	if i.Id == "" {
		return Issue{}, fmt.Errorf("github: issue %s/%s#%d not found", owner, repo, number)
	}
	return Issue{ID: i.Id, Number: i.Number, Title: i.Title, Body: i.Body, URL: i.Url, State: i.State}, nil
}

// PostComment adds a comment to an issue or pull request node (subjectID is the
// node id, e.g. Issue.ID from GetIssue).
func PostComment(ctx context.Context, client graphql.Client, subjectID, body string) error {
	_, err := AddIssueComment(ctx, client, subjectID, body)
	return err
}

// commentPageLimit bounds pagination so a misbehaving cursor cannot loop forever.
const commentPageLimit = 50

// CommentExists reports whether any of an issue's comments contains marker,
// paging through the comment connection until it finds one or runs out. It backs
// the sync adapter's at-least-once idempotency: a marker left by a prior post
// short-circuits a duplicate comment.
func CommentExists(ctx context.Context, client graphql.Client, owner, repo string, number int, marker string) (bool, error) {
	var after *string
	for page := 0; page < commentPageLimit; page++ {
		resp, err := IssueComments(ctx, client, owner, repo, number, after)
		if err != nil {
			return false, err
		}
		c := resp.Repository.Issue.Comments
		for i := range c.Nodes {
			if strings.Contains(c.Nodes[i].Body, marker) {
				return true, nil
			}
		}
		if !c.PageInfo.HasNextPage || c.PageInfo.EndCursor == "" {
			return false, nil
		}
		cur := c.PageInfo.EndCursor
		after = &cur
	}
	return false, nil
}
