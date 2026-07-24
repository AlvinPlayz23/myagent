#!/usr/bin/env bash
# myagent installer — curl -fsSL https://raw.githubusercontent.com/AlvinPlayz23/myagent/main/scripts/install.sh | bash
set -euo pipefail

REPO="AlvinPlayz23/myagent"
INSTALL_DIR="${MYAGENT_DIR:-$HOME/.myagent}/bin"
BINDIR="${BINDIR:-/usr/local/bin}"

info()  { printf "\033[34m•\033[0m %s\n" "$1"; }
ok()    { printf "\033[32m✓\033[0m %s\n" "$1"; }
err()   { printf "\033[31m✗\033[0m %s\n" "$1"; }
warn()  { printf "\033[33m⚠\033[0m %s\n" "$1"; }

# ---- detect platform ----
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$OS" in
  linux)   EXT="tar.gz" ;;
  darwin)  EXT="tar.gz" ;;
  mingw*|msys*|cygwin*) OS="windows"; EXT="zip" ;;
  *)       err "unsupported OS: $OS"; exit 1 ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)            err "unsupported arch: $ARCH"; exit 1 ;;
esac

# ---- try 1: download pre-built binary from GitHub Releases ----
info "Checking for pre-built binary…"
VERSION=$(curl -sSfL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null \
  | grep '"tag_name"' \
  | cut -d'"' -f4)

if [ -n "$VERSION" ]; then
  ARCHIVE="myagent_${VERSION}_${OS}_${ARCH}.${EXT}"
  URL="https://github.com/$REPO/releases/download/$VERSION/$ARCHIVE"

  mkdir -p "$INSTALL_DIR"
  cd "$INSTALL_DIR"

  if curl -fsSL "$URL" -o "$ARCHIVE" 2>/dev/null; then
    if [ "$EXT" = "zip" ]; then
      unzip -oq "$ARCHIVE" && rm "$ARCHIVE"
    else
      tar -xzf "$ARCHIVE" && rm "$ARCHIVE"
    fi

    # symlink
    if [ -w "$BINDIR" ]; then
      ln -sf "$INSTALL_DIR/myagent" "$BINDIR/myagent"
      ok "Installed myagent $VERSION to $BINDIR/myagent"
    else
      echo ""
      echo "  Add myagent to your PATH:"
      echo "    export PATH=\"\$PATH:$INSTALL_DIR\""
      ok "Installed myagent $VERSION to $INSTALL_DIR"
    fi
    echo ""
    echo "  Run:  myagent"
    exit 0
  else
    rm -f "$ARCHIVE"
    warn "No binary release found for your platform."
  fi
fi

# ---- try 2: fall back to go install ----
warn "Falling back to 'go install' (requires Go 1.26+)..."
if ! command -v go &>/dev/null; then
  err "Go is not installed. Install Go from https://go.dev/dl and try again."
  exit 1
fi

# Check Go version
GO_VERSION=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+')
if [ "$(echo "$GO_VERSION" | cut -d. -f1)" -lt 1 ] || { [ "$(echo "$GO_VERSION" | cut -d. -f1)" -eq 1 ] && [ "$(echo "$GO_VERSION" | cut -d. -f2)" -lt 26 ]; }; then
  err "Go 1.26+ required (found $GO_VERSION). Upgrade from https://go.dev/dl"
  exit 1
fi

info "Running: go install github.com/$REPO@latest"
go install "github.com/$REPO@latest"

# Ensure GOBIN is on PATH or symlink
GOBIN=$(go env GOBIN)
[ -z "$GOBIN" ] && GOBIN="$(go env GOPATH)/bin"

if [ -f "$GOBIN/myagent" ]; then
  if [ -w "$BINDIR" ]; then
    ln -sf "$GOBIN/myagent" "$BINDIR/myagent"
    ok "Installed myagent to $BINDIR/myagent"
  else
    echo ""
    echo "  Add myagent to your PATH:"
    echo "    export PATH=\"\$PATH:$GOBIN\""
    ok "Installed myagent to $GOBIN"
  fi
fi

echo ""
echo "  Run:  myagent"
echo "  Help: myagent --help"
