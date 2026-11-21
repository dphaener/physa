package github

import "strings"

// Login returns the login of the GitHub account gh is signed in as. It is
// not part of forge.Forge: only the person running Physa needs it. Failures
// are classed as gh's others are, so a missing gh is forge.ErrUnavailable and
// a signed-out one forge.ErrUnauthenticated.
func (c *Client) Login() (string, error) {
	out, err := c.gh("api", "user", "--jq", ".login")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
