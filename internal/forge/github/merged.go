package github

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/dphaener/physa/internal/forge"
)

// mergedPage is how many PRs one query asks for.
const mergedPage = 25

// mergedQuery asks for a page of merged PRs, the most recently updated first,
// with their conversation, reviews (with state) and inline comments, and the
// issues each closes.
const mergedQuery = `query($owner: String!, $name: String!, $cursor: String) {
  repository(owner: $owner, name: $name) {
    pullRequests(states: MERGED, orderBy: {field: UPDATED_AT, direction: DESC}, first: 25, after: $cursor) {
      pageInfo { hasNextPage endCursor }
      nodes {
        number
        title
        body
        mergedAt
        createdAt
        mergeCommit { oid }
        updatedAt
        author { login }
        mergedBy { login }
        comments(first: 100) { nodes { author { login __typename } createdAt body } }
        reviews(first: 100) {
          nodes {
            author { login __typename }
            state
            submittedAt
            body
            comments(first: 100) { nodes { author { login __typename } createdAt body } }
          }
        }
        commits(first: 100) { nodes { commit { oid committedDate statusCheckRollup { state } } } }
        closingIssuesReferences(first: 10) { nodes { number title body } }
      }
    }
  }
}`

type mergedNode struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	MergedAt  time.Time `json:"mergedAt"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Merge     *struct {
		OID string `json:"oid"`
	} `json:"mergeCommit"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				OID           string    `json:"oid"`
				CommittedDate time.Time `json:"committedDate"`
				Rollup        *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
	Author   *author `json:"author"`
	MergedBy *author `json:"mergedBy"`
	Comments struct {
		Nodes []comment `json:"nodes"`
	} `json:"comments"`
	Reviews struct {
		Nodes []review `json:"nodes"`
	} `json:"reviews"`
	Issues struct {
		Nodes []forge.Issue `json:"nodes"`
	} `json:"closingIssuesReferences"`
}

type mergedResponse struct {
	Data struct {
		Repository *struct {
			PullRequests struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []mergedNode `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// MergedPRs returns the PRs merged at or after since, newest merge first, at
// most max, with their people's review and the issues they close. more is
// true when max stopped the list before since did. A max below 1 returns
// nothing. It asks MergedPage page after page and never waits: a rate limit
// comes back as its *forge.RateLimit.
func (c *Client) MergedPRs(since time.Time, max int) ([]forge.PR, bool, error) {
	if max < 1 {
		return []forge.PR{}, false, nil
	}
	var prs []forge.PR
	more := false
	cursor := ""
	for {
		page, err := c.MergedPage(since, cursor)
		if err != nil {
			return nil, false, err
		}
		prs = append(prs, page.PRs...)
		exhausted := page.Next == ""
		if len(prs) > max || (len(prs) == max && !exhausted) {
			more = true
		}
		if exhausted || len(prs) >= max {
			break
		}
		cursor = page.Next
	}
	sort.SliceStable(prs, func(i, j int) bool { return prs[i].MergedAt.After(prs[j].MergedAt) })
	if len(prs) > max {
		prs = prs[:max]
	}
	if prs == nil {
		prs = []forge.PR{}
	}
	return prs, more, nil
}

// MergedPage returns one page of the PRs merged at or after since, starting
// at cursor ("" for the first page), in GitHub's order: most recently
// updated first. GitHub orders pages by update time, and a PR merged after
// since was updated after it, so Next is "" at the first page that reaches
// back past since, at an empty page and at the last page. A rate limit,
// whether GitHub names it in the response or gh names it on stderr, is a
// *forge.RateLimit; MergedPage never waits or retries.
func (c *Client) MergedPage(since time.Time, cursor string) (forge.Page, error) {
	owner, name, err := c.repo()
	if err != nil {
		return forge.Page{}, err
	}
	args := []string{"api", "graphql", "--include", "-f", "query=" + mergedQuery, "-f", "owner=" + owner, "-f", "name=" + name}
	if cursor != "" {
		args = append(args, "-f", "cursor="+cursor)
	}
	out, err := c.run(c.dir, nil, args...)
	r := splitInclude(out)
	if limit := rateLimit(r); limit != nil {
		return forge.Page{}, limit
	}
	var resp mergedResponse
	if jerr := json.Unmarshal(r.body, &resp); jerr == nil && len(resp.Errors) > 0 {
		return forge.Page{}, fmt.Errorf("GitHub GraphQL: %s", resp.Errors[0].Message)
	} else if err != nil {
		if limit := stderrLimit(err); r.status == 0 && limit != nil {
			return forge.Page{}, limit
		}
		return forge.Page{}, classify(err)
	} else if jerr != nil {
		return forge.Page{}, fmt.Errorf("GitHub GraphQL: %w", jerr)
	}
	if resp.Data.Repository == nil {
		return forge.Page{}, fmt.Errorf("GitHub GraphQL: no repository %s/%s", owner, name)
	}
	page := resp.Data.Repository.PullRequests
	pg := forge.Page{PRs: []forge.PR{}}
	for _, n := range page.Nodes {
		if !n.MergedAt.Before(since) {
			pg.PRs = append(pg.PRs, n.pr())
		}
	}
	crossed := len(page.Nodes) == 0 || page.Nodes[len(page.Nodes)-1].UpdatedAt.Before(since)
	if !crossed && page.PageInfo.HasNextPage {
		pg.Next = page.PageInfo.EndCursor
	}
	return pg, nil
}

func (n mergedNode) pr() forge.PR {
	pr := forge.PR{
		Number: n.Number, Title: n.Title, Body: n.Body, MergedAt: n.MergedAt, CreatedAt: n.CreatedAt,
		Comments: comments(n.Comments.Nodes, n.Reviews.Nodes),
		Commits:  []forge.Commit{},
		Issues:   n.Issues.Nodes,
	}
	if n.Author != nil {
		pr.Author = n.Author.Login
	}
	if n.Merge != nil {
		pr.MergeCommit = n.Merge.OID
	}
	if n.MergedBy != nil {
		pr.MergedBy = n.MergedBy.Login
	}
	for _, cn := range n.Commits.Nodes {
		c := forge.Commit{ID: cn.Commit.OID, At: cn.Commit.CommittedDate}
		if cn.Commit.Rollup != nil {
			c.Checks = cn.Commit.Rollup.State
		}
		pr.Commits = append(pr.Commits, c)
	}
	sort.SliceStable(pr.Commits, func(i, j int) bool { return pr.Commits[i].At.Before(pr.Commits[j].At) })
	return pr
}
