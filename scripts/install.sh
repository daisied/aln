#!/usr/bin/env bash
set -euo pipefail

BINARY_NAME="${ALN_BINARY_NAME:-aln}"
REPO="${ALN_REPO:-daisied/aln}"
VERSION="${ALN_VERSION:-latest}"
INSTALL_DIR="${ALN_INSTALL_DIR:-$HOME/.local/bin}"

if ! command -v curl >/dev/null 2>&1; then
  echo "error: curl is required." >&2
  exit 1
fi

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
  linux|darwin) ;;
  *)
    echo "error: unsupported OS: $OS" >&2
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "error: unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

if [[ "$VERSION" == "latest" ]]; then
  API_URL="https://api.github.com/repos/$REPO/releases/latest"
else
  API_URL="https://api.github.com/repos/$REPO/releases/tags/$VERSION"
fi

echo "Fetching release metadata for $REPO ($VERSION)..."
RELEASE_JSON="$(curl -fsSL "$API_URL")"

TAG="$(
  printf '%s' "$RELEASE_JSON" \
    | grep -m1 '"tag_name"' \
    | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/'
)"

if [[ -z "$TAG" ]]; then
  echo "error: could not read release tag from GitHub API." >&2
  exit 1
fi

match_asset_url() {
  local pattern="$1"
  printf '%s' "$RELEASE_JSON" \
    | grep '"browser_download_url"' \
    | sed -E 's/.*"browser_download_url"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/' \
    | grep -E "$pattern" \
    | head -n1
}

ASSET_URL=""
for pattern in \
  "${BINARY_NAME}[-_]${OS}[-_]${ARCH}\\.tar\\.gz$" \
  "${BINARY_NAME}[-_]${OS}[-_]${ARCH}\\.tgz$" \
  "${BINARY_NAME}[-_]${OS}[-_]${ARCH}$"
do
  ASSET_URL="$(match_asset_url "$pattern" || true)"
  if [[ -n "$ASSET_URL" ]]; then
    break
  fi
done

if [[ -z "$ASSET_URL" ]]; then
  echo "error: no matching release asset found for ${OS}/${ARCH}." >&2
  echo "expected patterns like ${BINARY_NAME}-${OS}-${ARCH}[.tar.gz]" >&2
  exit 1
fi

TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

ASSET_FILE="$TMP_DIR/asset"
echo "Downloading $ASSET_URL"
curl -fL "$ASSET_URL" -o "$ASSET_FILE"

BIN_PATH=""
if [[ "$ASSET_URL" == *.tar.gz || "$ASSET_URL" == *.tgz ]]; then
  tar -xzf "$ASSET_FILE" -C "$TMP_DIR"
  if [[ -f "$TMP_DIR/$BINARY_NAME" ]]; then
    BIN_PATH="$TMP_DIR/$BINARY_NAME"
  else
    BIN_PATH="$(find "$TMP_DIR" -type f -name "$BINARY_NAME" | head -n1 || true)"
  fi
else
  BIN_PATH="$ASSET_FILE"
fi

if [[ -z "$BIN_PATH" || ! -f "$BIN_PATH" ]]; then
  echo "error: could not locate extracted binary '$BINARY_NAME'." >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
install -m 0755 "$BIN_PATH" "$INSTALL_DIR/$BINARY_NAME"
echo "Installed: $INSTALL_DIR/$BINARY_NAME"

PATH_EXPORT="export PATH=\"$INSTALL_DIR:\$PATH\""
NEXT_STEP_CMD=""
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
  RC_FILE=""
  if [[ -n "${ZSH_VERSION:-}" ]]; then
    RC_FILE="$HOME/.zshrc"
  elif [[ -n "${BASH_VERSION:-}" ]]; then
    RC_FILE="$HOME/.bashrc"
  elif [[ -f "$HOME/.profile" ]]; then
    RC_FILE="$HOME/.profile"
  else
    RC_FILE="$HOME/.bashrc"
  fi

  if [[ ! -f "$RC_FILE" ]] || ! grep -Fq "$PATH_EXPORT" "$RC_FILE"; then
    printf '\n%s\n' "$PATH_EXPORT" >> "$RC_FILE"
    echo "Added $INSTALL_DIR to PATH in $RC_FILE"
    NEXT_STEP_CMD="source $RC_FILE"
  fi
fi

echo "$BINARY_NAME $TAG installed successfully."
if [[ -n "$NEXT_STEP_CMD" ]]; then
  echo
  echo "Next step (run this in your shell):"
  echo "  $NEXT_STEP_CMD"
  echo
fi
