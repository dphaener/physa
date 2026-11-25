# Install · Rules

## Contents

- [Every merge releases](#every-merge-releases)
- [Installs are checked and need no sudo](#installs-are-checked-and-need-no-sudo)

## Entries

### Every merge releases
`merge-releases` · from docs/architecture/11-install.md
Each merge to main publishes a release, tagged `v0.0.N` and marked latest, with binaries for macOS and Linux on amd64 and arm64 and a checksums file. Each binary reports its version.
Because: people should be able to try what is on main without building it.

### Installs are checked and need no sudo
`install-safe` · from docs/architecture/11-install.md
The install script checks the tarball against the release's checksums before installing, installs to `~/.local/bin` unless told otherwise, and never asks for sudo.
Because: a script piped into a shell must not install what it didn't check or reach beyond the person's own files.
