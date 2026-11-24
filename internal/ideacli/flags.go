package ideacli

import (
	"github.com/spf13/pflag"

	"github.com/dphaener/physa/internal/command"
)

// JSON declares the --json switch on fs, bound to on and off by default.
func JSON(fs *pflag.FlagSet, on *bool) { command.Switch(fs, "json", on, "") }

// String declares --name on fs as a flag with a value, bound to v and empty
// by default; the glue rejects an empty value typed on the line.
func String(fs *pflag.FlagSet, name string, v *string) { fs.StringVar(v, name, "", "") }
