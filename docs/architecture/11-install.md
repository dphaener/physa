# Architecture 11: install

Draft 0.1, 2026-11-25; settled with the owner. Uses `.physa/glossary.md` terms only. Serves the pursuit `add-a-release-pipeline-so`, whose signal is that every merge to main releases a binary that runs on macOS and Linux, and that people download it.

Today the only way to run Physa is to install Go and build it. This design releases a binary on every merge and gives people one line to install it.

## Settled with the owner (2026-11-25)

- **A capability of its own, `install`**, accepting a proposal: "Anyone can install and run Physa in minutes, without Go." It holds the release and the install script. Rejected: counting it under `init`, whose promise is about the map, not the binary.
- **GoReleaser builds the release**, choosing an option over the proposal. It builds macOS and Linux for amd64 and arm64, packs a tarball of each with the licence and notice, writes a checksums file and publishes a GitHub release. The modernc SQLite driver is pure Go, so one Linux runner cross-compiles every target with CGO off. Rejected: a hand-written workflow of a go build matrix and gh, with no new dependency but more YAML to keep.
- **Every merge to main releases**, from the pursuit's claim. Releases are tagged `v0.0.N`, counting up from the newest such tag, and marked latest; the owner decides when Physa reaches 0.1. The version goes into the binary as `main.version`. Rejected: date-based tags, which stop looking like semver.
- **The script installs to `~/.local/bin`**, accepting a proposal: `curl -fsSL https://raw.githubusercontent.com/dphaener/physa/main/install.sh | sh`. It needs no sudo, checks the tarball against the checksums file, takes `PHYSA_INSTALL_DIR` for another place, and says how to put the directory on PATH when it isn't. Rejected: `/usr/local/bin`, which needs sudo on most Linux machines and newer Macs.
- **The signal is read from GitHub's download counts.** Every release asset carries a `download_count` in GitHub's API, so the resolver can read it as well as judge it.
