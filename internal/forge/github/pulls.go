package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// OpenPR returns the open PR whose head is the branch head. Its bool is
// false when none is open.
func (c *Client) OpenPR(ctx context.Context, head string) (n int, url string, ok bool, err error) {
	out, err := c.gh(ctx, "pr", "list", "--head", head, "--state", "open", "--json", "number,url", "--limit", "1")
	if err != nil {
		return 0, "", false, err
	}
	var prs []struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(out, &prs); err != nil {
		return 0, "", false, fmt.Errorf("gh pr list: %w", err)
	}
	if len(prs) == 0 {
		return 0, "", false, nil
	}
	return prs[0].Number, prs[0].URL, true, nil
}

// CreatePR opens a PR from branch head into branch base, with body on gh's
// stdin. It returns the new PR's number and URL.
func (c *Client) CreatePR(ctx context.Context, head, base, title, body string) (n int, url string, err error) {
	out, err := c.ghIn(ctx, []byte(body), "pr", "create", "--head", head, "--base", base,
		"--title", title, "--body-file", "-")
	if err != nil {
		return 0, "", err
	}
	// gh prints the new PR's URL, ending in its number.
	url = strings.TrimSpace(string(out))
	if i := strings.LastIndex(url, "/"); i >= 0 {
		n, _ = strconv.Atoi(url[i+1:])
	}
	if n == 0 {
		return 0, "", fmt.Errorf("gh pr create: no PR number in %q", url)
	}
	return n, url, nil
}

// EditPR replaces the body of PR n, with the body on gh's stdin.
func (c *Client) EditPR(ctx context.Context, n int, body string) error {
	_, err := c.ghIn(ctx, []byte(body), "pr", "edit", strconv.Itoa(n), "--body-file", "-")
	return err
}
