#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$SCRIPT_DIR/../.."

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}Starting Leros Dev Frontend...${NC}"

ENV_FILE="$SCRIPT_DIR/.env"
if [ -f "$ENV_FILE" ]; then
    echo -e "${GREEN}Loading environment from .env${NC}"
    set -a
    source "$ENV_FILE"
    set +a
else
    echo -e "${YELLOW}.env file not found. Run ./dev-setup.sh or copy .env.example to .env.${NC}"
fi

if [ ! -d "$ROOT_DIR/frontend" ]; then
    echo -e "${RED}Error: Frontend directory not found at: $ROOT_DIR/frontend${NC}"
    exit 1
fi

# Detect package manager: pnpm > npm
PKG_MANAGER=""
if command -v pnpm &> /dev/null; then
    PKG_MANAGER="pnpm"
    echo -e "${GREEN}Using pnpm${NC}"
elif command -v npm &> /dev/null; then
    PKG_MANAGER="npm"
    echo -e "${GREEN}Using npm${NC}"
else
    echo -e "${RED}Error: Neither pnpm nor npm found. Please install Node.js first.${NC}"
    exit 1
fi

cd "$ROOT_DIR/frontend"

if [ ! -d "node_modules" ]; then
    echo -e "${YELLOW}Installing dependencies...${NC}"
    $PKG_MANAGER install
fi

echo -e "${GREEN}Starting frontend dev server...${NC}"
echo -e "${BLUE}Note: Configure frontend to connect to backend at http://localhost:8080${NC}"
echo -e "${BLUE}Web frontend should be available at http://localhost:3005${NC}"

$PKG_MANAGER run dev:web
