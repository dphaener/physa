package person

import (
	"context"
	"fmt"
	"io"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/vcs/git"
)

// ForRepo names the person running Physa in the repository at dir: their
// setting, else the login of the account f's forge signs them in as, else
// their git email. When no name can be had it says why on stderr, once, and
// returns "": the person is unknown, never guessed.
func ForRepo(dir string, f forge.Forge, stderr io.Writer) string {
	cfg, err := ConfigDir()
	if err == nil {
		o := Options{ConfigDir: cfg, Email: func() (string, error) { return git.Email(context.TODO(), dir) }}
		if l, ok := f.(interface{ Login() (string, error) }); ok {
			o.Login = l.Login
		}
		var n Name
		if n, err = Resolve(o); err == nil {
			return n.Name
		}
	}
	fmt.Fprintf(stderr, "physa: no person named: %v\n", err)
	return ""
}
