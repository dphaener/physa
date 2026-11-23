package github

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ChangeLogins asks GitHub, 100 changes a query, for each change's author's
// login. A change GitHub does not know, or whose author has no account, is
// left out.
func (c *Client) ChangeLogins(ids []string) (map[string]string, error) {
	owner, name, err := c.repo()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for start := 0; start < len(ids); start += 100 {
		batch := ids[start:min(start+100, len(ids))]
		var q strings.Builder
		q.WriteString("query($owner:String!,$name:String!){repository(owner:$owner,name:$name){")
		for i, id := range batch {
			fmt.Fprintf(&q, "c%d:object(oid:%q){...on Commit{author{user{login}}}}", i, id)
		}
		q.WriteString("}}")
		raw, err := c.gh("api", "graphql", "-f", "query="+q.String(), "-f", "owner="+owner, "-f", "name="+name)
		if err != nil {
			return nil, err
		}
		var resp struct {
			Data struct {
				Repository map[string]*struct {
					Author struct {
						User *struct {
							Login string `json:"login"`
						} `json:"user"`
					} `json:"author"`
				} `json:"repository"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("GitHub GraphQL: %w", err)
		}
		for i, id := range batch {
			if o := resp.Data.Repository[fmt.Sprintf("c%d", i)]; o != nil && o.Author.User != nil && o.Author.User.Login != "" {
				out[id] = o.Author.User.Login
			}
		}
	}
	return out, nil
}
