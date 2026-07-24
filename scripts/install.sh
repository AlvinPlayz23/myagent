#!/usr/bin/env bash
# myagent installer — curl -fsSL https://myagent.dev/install.sh | bash
set -euo pipefail

REPO="AlvinPlayz23/myagent"
INSTALL_DIR="${MYAGENT_DIR:-$HOME/.myagent}/bin"
BINDIR="${BINDIR:-/usr/local/bin}"

# ---- helpers ----
info()  { printf "\033[34m•\033[0m %s\n" "$1"; }
ok()    { printf "\033[32m✓\033[0m %s\n" "$1"; }
err()   { printf "\033[31m✗\033[0m %s\n" "$1"; exit 1; }

# ---- detect platform ----
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$OS" in
  linux)   EXT="tar.gz" ;;
  darwin)  EXT="tar.gz" ;;
  mingw*|msys*|cygwin*) OS="windows"; EXT="zip" ;;
  *)       err "unsupported OS: $OS" ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)            err "unsupported arch: $ARCH" ;;
esac

# ---- resolve latest version ----
info "Resolving latest release…"
VERSION=$(curl -sSfL "https://api.github.com/repos/$REPO/releases/latest" \
  | grep '"tag_name"' \
  | cut -d'"' -f4)
[ -n "$VERSION" ] || err "could not determine latest version"

# ---- download ----
ARCHIVE="myagent_${VERSION}_${OS}_${ARCH}.${EXT}"
URL="https://github.com/$REPO/releases/download/$VERSION/$ARCHIVE"
info "Downloading $URL"

mkdir -p "$INSTALL_DIR"
cd "$INSTALL_DIR"
curl -fsSL "$URL" -o "$ARCHIVE"

# ---- extract ----
if [ "$EXT" = "zip" ]; then
  unzip -oq "$ARCHIVE" && rm "$ARCHIVE"
else
  tar -xzf "$ARCHIVE" && rm "$ARCHIVE"
fi

# ---- symlink ----
if [ -w "$BINDIR" ]; then
  ln -sf "$INSTALL_DIR/myagent" "$BINDIR/myagent"
  ok "Installed myagent $VERSION to $BINDIR/myagent"
else
  # fallback: just print instructions
  echo ""
  echo "  Add myagent to your PATH:"
  echo "    export PATH=\"\$PATH:$INSTALL_DIR\""
  ok "Installed myagent $VERSION to $INSTALL_DIR"
fi

# ---- post-install hint ----
echo ""
echo "  Run:  myagent"
echo "  Help: myagent --help"
