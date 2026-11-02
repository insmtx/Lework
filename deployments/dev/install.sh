#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
BIN_SRC="$ROOT_DIR/bundles/lework"

# ---- helpers ----

red()  { echo -e "\033[31m$*\033[0m"; }
green(){ echo -e "\033[32m$*\033[0m"; }
bold() { echo -e "\033[1m$*\033[0m"; }

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin";;
    Linux)  echo "linux";;
    MINGW*|MSYS*|CYGWIN*) echo "windows";;
    *)      echo "unknown";;
  esac
}

check_path() {
  local dir="$1"
  if [[ ":$PATH:" != *":$dir:"* ]]; then
    red "WARNING: $dir is not in your PATH."
    echo "Add the following line to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
    bold "  export PATH=\"$dir:\$PATH\""
    return 1
  fi
  return 0
}

# ---- install ----

do_install() {
  local os="$1"

  if [ ! -f "$BIN_SRC" ]; then
    echo "Binary not found, building..."
    cd "$ROOT_DIR"
    go build -v -o "$BIN_SRC" ./backend/cmd/lework/
  fi

  case "$os" in
    darwin|linux)
      local dest_dir="$HOME/.local/bin"
      local dest="$dest_dir/lework"
      mkdir -p "$dest_dir"
      ln -sf "$BIN_SRC" "$dest"
      green "→ $dest"
      check_path "$dest_dir" || true
      ;;
    windows)
      local dest_dir="$APPDATA/lework/bin"
      local dest="$dest_dir/lework.exe"
      mkdir -p "$dest_dir"
      cp -f "$BIN_SRC" "$dest"
      green "→ $dest"
      red "WARNING: Add this directory to your user PATH:"
      bold "  $dest_dir"
      ;;
    *)
      red "Unsupported OS: $(uname -s)"
      exit 1
      ;;
  esac

  green "Done. Run 'lework --help' to verify."
}

# ---- uninstall ----

do_uninstall() {
  local os="$1"

  case "$os" in
    darwin|linux)
      local dest="$HOME/.local/bin/lework"
      if [ -f "$dest" ] || [ -L "$dest" ]; then
        rm -f "$dest"
        green "Removed $dest"
      else
        echo "Not installed: $dest"
      fi
      ;;
    windows)
      local dest="$APPDATA/lework/bin/lework.exe"
      if [ -f "$dest" ]; then
        rm -f "$dest"
        green "Removed $dest"
      else
        echo "Not installed: $dest"
      fi
      ;;
    *)
      red "Unsupported OS: $(uname -s)"
      exit 1
      ;;
  esac
}

# ---- main ----

OS="$(detect_os)"

if [ "${1:-}" == "--uninstall" ]; then
  do_uninstall "$OS"
else
  do_install "$OS"
fi
