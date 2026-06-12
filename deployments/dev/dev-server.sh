#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$SCRIPT_DIR/../.."

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

BUILD_FLAG=false
if [ "$1" = "--build" ] || [ "$1" = "-b" ]; then
    BUILD_FLAG=true
fi

echo -e "${BLUE}Starting Leros Dev Server...${NC}"

ENV_FILE="$SCRIPT_DIR/.env"
if [ -f "$ENV_FILE" ]; then
    echo -e "${GREEN}Loading environment from .env${NC}"
    set -a
    source "$ENV_FILE"
    set +a
else
    echo -e "${YELLOW}.env file not found. Run ./dev-setup.sh or copy .env.example to .env.${NC}"
fi

# Ensure config file exists (copy from template if not)
CONFIG_FILE="$SCRIPT_DIR/server.config.yaml"
CONFIG_TEMPLATE="$SCRIPT_DIR/server.config.example.yaml"
if [ ! -f "$CONFIG_FILE" ]; then
    echo -e "${YELLOW}Config file not found. Creating from template...${NC}"
    cp "$CONFIG_TEMPLATE" "$CONFIG_FILE"
    echo -e "${GREEN}server.config.yaml created from server.config.example.yaml${NC}"
    echo -e "${YELLOW}Please review and edit the config before proceeding.${NC}"
    echo -e "${YELLOW}Press Enter to continue, or Ctrl+C to abort...${NC}"
    read -r
fi

# Build the binary (auto-detect source changes)
NEED_BUILD=false
if [ ! -f "$ROOT_DIR/bundles/leros" ] || [ "$BUILD_FLAG" = true ]; then
    NEED_BUILD=true
else
    LATEST_SRC=$(find "$ROOT_DIR/backend/cmd/leros" -name "*.go" -exec stat -f "%m" {} \; 2>/dev/null | sort -rn | head -1)
    BINARY_MTIME=$(stat -f "%m" "$ROOT_DIR/bundles/leros" 2>/dev/null)
    if [ -n "$LATEST_SRC" ] && [ -n "$BINARY_MTIME" ] && [ "$LATEST_SRC" -gt "$BINARY_MTIME" ]; then
        NEED_BUILD=true
        echo -e "${YELLOW}Source changed, auto-rebuilding...${NC}"
    fi
fi

if [ "$NEED_BUILD" = true ]; then
    echo -e "${YELLOW}Building Leros binary...${NC}"
    cd "$ROOT_DIR"
    go build -v -o ./bundles/leros ./backend/cmd/leros/
    echo -e "${GREEN}Build complete${NC}"
else
    echo -e "${GREEN}Using existing Leros binary. Use --build to force rebuild.${NC}"
fi

echo -e "${BLUE}Starting server (port 8080)...${NC}"

LEROS_STORAGE_LOCAL_DIR="$ROOT_DIR/leros-storage"
mkdir -p "$LEROS_STORAGE_LOCAL_DIR"

cd "$ROOT_DIR"
LEROS_STORAGE_LOCAL_DIR="$LEROS_STORAGE_LOCAL_DIR" ./bundles/leros server --config "$CONFIG_FILE" --workspace-root "$ROOT_DIR/.leros-workspace/1/1/workspace"
