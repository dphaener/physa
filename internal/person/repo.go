package person

import (
	"context"
	"fmt"
	"io"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/proc"
	"github.com/dphaener/physa/internal/vcs/git"
)

// ForRepo names the person running Physa in the repository at dir: their
// setting, else the login of the account f's forge signs them in as, else
// their git email. When no name can be had it says why on stderr, once, and
// returns "": the person is unknown, never guessed. st is the command's
// process state: its environment gives the setting's directory and is the
// one git runs in.
func ForRepo(ctx context.Context, st proc.State, dir string, f forge.Forge, stderr io.Writer) string {
	cfg, err := ConfigDir(st.Getenv)
	if err == nil {
		o := Options{ConfigDir: cfg, Email: func() (string, error) { return git.Email(ctx, dir, st.Environ()) }}
		if l, ok := f.(interface {
			Login(context.Context) (string, error)
		}); ok {
			o.Login = func() (string, error) { return l.Login(ctx) }
		}
		var n Name
		if n, err = Resolve(o); err == nil {
			return n.Name
		}
	}
	fmt.Fprintf(stderr, "physa: no person named: %v\n", err)
	return ""
}
