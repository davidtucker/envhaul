#!/bin/sh
# Install envhaul from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/davidtucker/envhaul/main/install.sh | sh
#
# Environment:
#   ENVHAUL_VERSION      version to install, e.g. v0.3.1 (default: latest)
#   ENVHAUL_INSTALL_DIR  install directory (default: /usr/local/bin, or ~/.local/bin if not writable)
#
# The archive is verified against checksums.txt. If cosign is installed, the
# checksums file's keyless signature is verified too.
set -eu

REPO="davidtucker/envhaul"
VERSION="${ENVHAUL_VERSION:-latest}"

say() { printf '%s\n' "$*" >&2; }
fail() { say "envhaul install: $*"; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"; }
need curl
need tar
need uname

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux | darwin) ;;
  *) fail "unsupported OS: $os (on Windows use scoop or download the zip from GitHub Releases)" ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) fail "unsupported architecture: $arch" ;;
esac

if [ "$VERSION" = "latest" ]; then
  VERSION=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's#.*/tag/##')
  [ -n "$VERSION" ] || fail "could not determine the latest version"
fi
case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac
num=${VERSION#v}

archive="envhaul_${num}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$VERSION"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Downloading envhaul $VERSION ($os/$arch)..."
curl -fsSL -o "$tmp/$archive" "$base/$archive" || fail "download failed: $base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || fail "download failed: checksums.txt"

if command -v cosign >/dev/null 2>&1; then
  curl -fsSL -o "$tmp/checksums.txt.sigstore.json" "$base/checksums.txt.sigstore.json" ||
    fail "download failed: checksums.txt.sigstore.json"
  cosign verify-blob \
    --bundle "$tmp/checksums.txt.sigstore.json" \
    --certificate-identity-regexp "^https://github.com/$REPO/" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    "$tmp/checksums.txt" >/dev/null 2>&1 || fail "signature verification of checksums.txt failed"
  say "Verified signature of checksums.txt"
else
  say "cosign not found; skipping signature verification (checksum is still verified)"
fi

expected=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)
[ -n "$expected" ] || fail "$archive not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
else
  actual=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" envhaul

dir="${ENVHAUL_INSTALL_DIR:-/usr/local/bin}"
if [ -z "${ENVHAUL_INSTALL_DIR:-}" ] && [ ! -w "$dir" ]; then
  dir="$HOME/.local/bin"
fi
mkdir -p "$dir"
install -m 0755 "$tmp/envhaul" "$dir/envhaul" 2>/dev/null || {
  cp "$tmp/envhaul" "$dir/envhaul" && chmod 0755 "$dir/envhaul"
}

say "Installed envhaul $VERSION to $dir/envhaul"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) say "Note: $dir is not on your PATH" ;;
esac
