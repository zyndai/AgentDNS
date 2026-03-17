#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}Agent DNS Cleanup Script${NC}"
echo -e "${BLUE}========================${NC}"
echo ""

# Function to print section headers
print_section() {
    echo -e "\n${BLUE}$1${NC}"
}

# Function to remove file or directory
remove_item() {
    local item=$1
    local description=$2
    
    if [ -e "$item" ]; then
        echo -e "${YELLOW}  Removing: ${description}${NC}"
        rm -rf "$item"
        echo -e "${GREEN}  ✓ Removed: ${item}${NC}"
    else
        echo -e "  Skipping: ${description} (not found)"
    fi
}

# Warning prompt
echo -e "${RED}WARNING: This will delete:${NC}"
echo "  • Built binaries (agentdns)"
echo "  • Downloaded ONNX models (~350MB)"
echo "  • All data directories (registry, logs, state)"
echo "  • Test artifacts"
echo "  • Docker volumes and containers"
echo "  • Temporary files"
echo ""
echo -e "${YELLOW}This action cannot be undone!${NC}"
echo ""
read -p "Are you sure you want to continue? (yes/no): " -r
echo ""

if [[ ! $REPLY =~ ^[Yy]es$ ]]; then
    echo -e "${RED}Cleanup cancelled.${NC}"
    exit 0
fi

echo -e "${GREEN}Starting cleanup...${NC}"

# 1. Built binaries
print_section "Removing built binaries..."
remove_item "./agentdns" "Agent DNS binary"
remove_item "./agentdns.exe" "Agent DNS Windows binary"

# 2. Go build cache and test artifacts
print_section "Removing Go build artifacts..."
remove_item "./coverage.out" "Coverage report"
remove_item "./coverage.html" "HTML coverage report"
remove_item "./*.test" "Test binaries"
remove_item "./*.prof" "Profiling data"

# 3. Downloaded models
print_section "Removing downloaded models..."
remove_item "$HOME/.agentdns/models" "ONNX models directory"

# 4. Data directories (local development)
print_section "Removing data directories..."
remove_item "./data" "Local data directory"
remove_item "./data-node-a" "Node A data"
remove_item "./data-node-b" "Node B data"
remove_item "./data-node-c" "Node C data"

# 5. Log files
print_section "Removing log files..."
remove_item "./logs" "Logs directory"
remove_item "./agentdns.log" "Main log file"
remove_item "./*.log" "All log files"

# 6. Temporary files
print_section "Removing temporary files..."
remove_item "./tmp" "Temporary directory"
remove_item "./*.tmp" "Temporary files"
remove_item "./.DS_Store" "macOS metadata"
remove_item "./**/.DS_Store" "All macOS metadata"

# 7. State files
print_section "Removing state files..."
remove_item "./state.json" "State file"
remove_item "./registry.db" "Registry database"
remove_item "./*.db" "All database files"
remove_item "./*.db-shm" "SQLite shared memory"
remove_item "./*.db-wal" "SQLite write-ahead log"

# 8. Docker cleanup (optional)
print_section "Docker cleanup..."
echo -e "${YELLOW}Do you want to remove Docker containers and volumes?${NC}"
read -p "This will stop and remove all Agent DNS containers (yes/no): " -r
echo ""

if [[ $REPLY =~ ^[Yy]es$ ]]; then
    echo "Stopping Docker Compose services..."
    docker-compose down -v 2>/dev/null || echo "  No standard docker-compose services running"
    docker-compose -f docker-compose.onnx.yml down -v 2>/dev/null || echo "  No ONNX docker-compose services running"
    
    echo "Removing Agent DNS Docker containers..."
    docker ps -a | grep agentdns | awk '{print $1}' | xargs -r docker rm -f 2>/dev/null || echo "  No containers found"
    
    echo "Removing Agent DNS Docker volumes..."
    docker volume ls | grep agentdns | awk '{print $2}' | xargs -r docker volume rm 2>/dev/null || echo "  No volumes found"
    
    echo "Removing Agent DNS Docker images..."
    docker images | grep agentdns | awk '{print $3}' | xargs -r docker rmi -f 2>/dev/null || echo "  No images found"
    
    echo -e "${GREEN}  ✓ Docker cleanup complete${NC}"
else
    echo "  Skipping Docker cleanup"
fi

# 9. System-wide installation cleanup (optional)
print_section "System-wide installation cleanup..."
echo -e "${YELLOW}Do you want to remove the system-wide installation?${NC}"
read -p "This will remove /usr/local/bin/agentdns and configs (requires sudo) (yes/no): " -r
echo ""

if [[ $REPLY =~ ^[Yy]es$ ]]; then
    if [ -f "/usr/local/bin/agentdns" ]; then
        echo "Removing /usr/local/bin/agentdns..."
        sudo rm -f /usr/local/bin/agentdns
        echo -e "${GREEN}  ✓ Removed system binary${NC}"
    else
        echo "  No system binary found"
    fi
    
    if [ -d "$HOME/.agentdns" ]; then
        echo "Removing $HOME/.agentdns directory..."
        rm -rf "$HOME/.agentdns"
        echo -e "${GREEN}  ✓ Removed user config directory${NC}"
    else
        echo "  No user config directory found"
    fi
else
    echo "  Skipping system-wide cleanup"
fi

# 10. Clean Go module cache (optional)
print_section "Go module cache..."
echo -e "${YELLOW}Do you want to clean the Go module cache for this project?${NC}"
read -p "This will remove cached dependencies (yes/no): " -r
echo ""

if [[ $REPLY =~ ^[Yy]es$ ]]; then
    echo "Cleaning Go module cache..."
    go clean -modcache 2>/dev/null || echo "  Failed to clean module cache (may require sudo)"
    echo -e "${GREEN}  ✓ Go module cache cleaned${NC}"
else
    echo "  Skipping Go module cache cleanup"
fi

# Summary
echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Cleanup Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "The following items were cleaned:"
echo "  ✓ Built binaries"
echo "  ✓ Build artifacts"
echo "  ✓ Downloaded models"
echo "  ✓ Data directories"
echo "  ✓ Log files"
echo "  ✓ Temporary files"
echo "  ✓ State files"
echo ""
echo "To rebuild the project, run:"
echo -e "  ${BLUE}./install.sh${NC}  (for full installation)"
echo -e "  ${BLUE}go build -o agentdns ./cmd/agentdns${NC}  (for quick build)"
echo ""
