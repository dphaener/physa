# Install · Decisions

## Contents

- [GoReleaser builds the release](#goreleaser-builds-the-release)
- [Releases count up from v0.0.1](#releases-count-up-from-v001)
- [Install to ~/.local/bin](#install-to-localbin)

## Entries

### GoReleaser builds the release
`goreleaser` · decided by dphaener, choosing an option · 2026-11-25 · from docs/architecture/11-install.md
GoReleaser builds, packs, checksums and publishes each release from one Linux runner, cross-compiling with CGO off.
Because: it is the standard tool for Go releases, and keeps the release to one small config.
Rejected:
- A hand-written workflow of a go build matrix and gh: no new dependency, but more YAML to keep.

### Releases count up from v0.0.1
`version-count` · decided by dphaener, accepting a proposal · 2026-11-25 · from docs/architecture/11-install.md
Every merge to main tags the next `v0.0.N` after the newest such tag. The owner decides when Physa reaches 0.1.
Because: every merge releases, and a counted patch version stays semver while Physa is early.
Rejected:
- Date-based tags such as v2026.11.25-1: fresh at a glance, but no longer semver.

### Install to ~/.local/bin
`install-dir` · decided by dphaener, accepting a proposal · 2026-11-25 · from docs/architecture/11-install.md
The script installs to `~/.local/bin`, or `PHYSA_INSTALL_DIR`, and says how to add it to PATH when it is missing.
Because: it needs no sudo on any machine.
Rejected:
- `/usr/local/bin`: on PATH everywhere, but needs sudo on most Linux machines and newer Macs.
