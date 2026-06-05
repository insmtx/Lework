#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}Leros Dev Environment Setup${NC}"
echo ""

echo -e "${BLUE}Checking prerequisites...${NC}"

if ! command -v docker &> /dev/null; then
    echo -e "${RED}Docker is not installed${NC}"
    exit 1
fi
echo -e "${GREEN}Docker: $(docker --version)${NC}"

if ! command -v docker-compose &> /dev/null; then
    echo -e "${RED}Docker Compose is not installed${NC}"
    exit 1
fi
echo -e "${GREEN}Docker Compose: $(docker-compose --version)${NC}"

echo ""

if [ ! -f "$SCRIPT_DIR/.env" ]; then
    echo -e "${YELLOW}Creating .env file from template...${NC}"
    cp "$SCRIPT_DIR/.env.example" "$SCRIPT_DIR/.env"
    echo -e "${GREEN}.env file created${NC}"
else
    echo -e "${GREEN}.env file already exists${NC}"
fi

for config in server worker; do
    if [ ! -f "$SCRIPT_DIR/$config.config.yaml" ]; then
        echo -e "${YELLOW}Creating $config.config.yaml from template...${NC}"
        cp "$SCRIPT_DIR/$config.config.example.yaml" "$SCRIPT_DIR/$config.config.yaml"
        echo -e "${GREEN}$config.config.yaml file created${NC}"
    else
        echo -e "${GREEN}$config.config.yaml file already exists${NC}"
    fi
done

echo ""
echo -e "${BLUE}Starting infrastructure services...${NC}"
cd "$SCRIPT_DIR"
docker-compose -f docker-compose.dev.yml up -d postgresql nats

echo -e "${YELLOW}Waiting for services to be healthy...${NC}"
sleep 5

for service in postgresql nats; do
    echo -n "Checking $service... "
    for i in {1..30}; do
        status=$(docker inspect --format='{{.State.Health.Status}}' leros-dev-$service 2>/dev/null || echo "starting")
        if [ "$status" = "healthy" ]; then
            echo -e "${GREEN}healthy${NC}"
            break
        fi
        if [ $i -eq 30 ]; then
            echo -e "${RED}timeout${NC}"
        fi
        sleep 2
    done
done

echo ""
echo -e "${GREEN}Dev environment setup complete!${NC}"
echo ""
echo -e "${BLUE}Next steps:${NC}"
echo "  1. Edit .env file and set your LLM_API_KEY"
echo "  2. Review server.config.yaml and worker.config.yaml"
echo "  3. Start server: ./dev-server.sh"
echo "  4. Start worker: ./dev-worker.sh"
