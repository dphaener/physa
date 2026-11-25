#!/bin/sh
# Install Physa's latest release (docs/architecture/11-install.md):
#
#   curl -fsSL https://raw.githubusercontent.com/dphaener/physa/main/install.sh | sh
#
# It installs physa to ~/.local/bin, or to PHYSA_INSTALL_DIR, never with sudo,
# and only after checking the download against the release's checksums.
set -eu

repo="dphaener/physa"
dir="${PHYSA_INSTALL_DIR:-$HOME/.local/bin}"

fail() {
	echo "physa install: $*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || fail "it needs $1, which isn't installed"
}

need curl
need tar

case "$(uname -s)" in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) fail "there's no release for $(uname -s); Physa runs on macOS and Linux" ;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) fail "there's no release for $(uname -m); Physa runs on amd64 and arm64" ;;
esac

if command -v sha256sum >/dev/null 2>&1; then
	sha() { sha256sum "$1" | cut -d ' ' -f 1; }
elif command -v shasum >/dev/null 2>&1; then
	sha() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
else
	fail "it needs sha256sum or shasum to check the download"
fi

tarball="physa_${os}_${arch}.tar.gz"
base="${PHYSA_RELEASE_URL:-https://github.com/$repo/releases/latest/download}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading $tarball"
curl -fsSL -o "$tmp/$tarball" "$base/$tarball" || fail "couldn't download $base/$tarball"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || fail "couldn't download the checksums"

want="$(awk -v f="$tarball" '$2 == f { print $1 }' "$tmp/checksums.txt")"
[ -n "$want" ] || fail "the release's checksums don't list $tarball"
[ "$(sha "$tmp/$tarball")" = "$want" ] || fail "$tarball doesn't match its checksum; nothing was installed"

tar -xzf "$tmp/$tarball" -C "$tmp" physa
mkdir -p "$dir"
mv "$tmp/physa" "$dir/physa"
chmod 755 "$dir/physa"

echo "Installed $("$dir/physa" version) to $dir/physa"
case ":$PATH:" in
*":$dir:"*) ;;
*) echo "$dir isn't on your PATH. Add it, for example: echo 'export PATH=\"$dir:\$PATH\"' >> ~/.profile" ;;
esac
