#!/bin/bash

# Agent DNS Installation Script
# Detects OS/Architecture, installs dependencies, and builds Agent DNS

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Print functions
print_header() {
    echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║${NC}  Agent DNS Installation Script                            ${BLUE}║${NC}"
    echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
    echo ""
}

print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Detect OS and Architecture
detect_system() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)
    
    case "$OS" in
        linux*)
            OS="linux"
            ;;
        darwin*)
            OS="darwin"
            ;;
        msys*|mingw*|cygwin*)
            OS="windows"
            ;;
        *)
            print_error "Unsupported operating system: $OS"
            exit 1
            ;;
    esac
    
    case "$ARCH" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        armv7l)
            ARCH="armv7"
            ;;
        *)
            print_error "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac
    
    print_info "Detected: $OS/$ARCH"
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Check Go installation
check_go() {
    print_info "Checking Go installation..."
    if ! command_exists go; then
        print_error "Go is not installed. Please install Go 1.24+ from https://go.dev/dl/"
        exit 1
    fi
    
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    print_success "Go $GO_VERSION found"
}

# Check Rust installation (required for ONNX)
check_rust() {
    if command_exists cargo && command_exists rustc; then
        RUST_VERSION=$(rustc --version | awk '{print $2}')
        print_success "Rust $RUST_VERSION found"
        return 0
    else
        return 1
    fi
}

# Install Rust
install_rust() {
    print_info "Rust is required for ONNX embedder support"
    read -p "Install Rust? (y/n): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        print_info "Installing Rust..."
        curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
        source "$HOME/.cargo/env"
        print_success "Rust installed successfully"
    else
        print_warning "Skipping Rust installation. ONNX embedder will not be available."
        return 1
    fi
}

# Prompt for embedding backend
choose_embedding_backend() {
    echo ""
    print_header
    echo -e "${BLUE}Choose Embedding Backend:${NC}"
    echo ""
    echo "  1) Hash Embedder (Fast, Zero Dependencies)"
    echo "     - Latency: <1ms"
    echo "     - Quality: Good for keyword matching"
    echo "     - Dependencies: None"
    echo ""
    echo "  2) ONNX Embedder (Best Quality, Local ML)"
    echo "     - Latency: 2-7ms"
    echo "     - Quality: Excellent semantic search"
    echo "     - Dependencies: Rust, tokenizers library"
    echo ""
    echo "  3) HTTP Embedder (External Service)"
    echo "     - Latency: 20-50ms"
    echo "     - Quality: Depends on service"
    echo "     - Dependencies: External embedding service (e.g., Ollama)"
    echo ""
    
    read -p "Enter choice [1-3] (default: 1): " EMBEDDING_CHOICE
    EMBEDDING_CHOICE=${EMBEDDING_CHOICE:-1}
    
    case $EMBEDDING_CHOICE in
        1)
            BACKEND="hash"
            USE_CGO=0
            print_info "Selected: Hash Embedder"
            ;;
        2)
            BACKEND="onnx"
            USE_CGO=1
            print_info "Selected: ONNX Embedder"
            ;;
        3)
            BACKEND="http"
            USE_CGO=0
            print_info "Selected: HTTP Embedder"
            ;;
        *)
            print_error "Invalid choice. Defaulting to Hash Embedder."
            BACKEND="hash"
            USE_CGO=0
            ;;
    esac
}

# Choose ONNX model (if ONNX backend selected)
choose_onnx_model() {
    if [ "$BACKEND" != "onnx" ]; then
        return
    fi
    
    echo ""
    echo -e "${BLUE}Choose ONNX Model:${NC}"
    echo ""
    echo "  1) all-MiniLM-L6-v2 (Fast, 90MB)"
    echo "     - Speed: ⚡⚡⚡ Fastest"
    echo "     - Quality: Good"
    echo "     - Languages: English"
    echo ""
    echo "  2) bge-small-en-v1.5 (Balanced, 130MB) [RECOMMENDED]"
    echo "     - Speed: ⚡⚡ Medium"
    echo "     - Quality: Better"
    echo "     - Languages: English"
    echo ""
    echo "  3) e5-small-v2 (Best Quality, 130MB)"
    echo "     - Speed: ⚡⚡ Medium"
    echo "     - Quality: Best"
    echo "     - Languages: Multilingual (9 languages)"
    echo ""
    
    read -p "Enter choice [1-3] (default: 2): " MODEL_CHOICE
    MODEL_CHOICE=${MODEL_CHOICE:-2}
    
    case $MODEL_CHOICE in
        1)
            ONNX_MODEL="all-MiniLM-L6-v2"
            ;;
        2)
            ONNX_MODEL="bge-small-en-v1.5"
            ;;
        3)
            ONNX_MODEL="e5-small-v2"
            ;;
        *)
            print_warning "Invalid choice. Using bge-small-en-v1.5"
            ONNX_MODEL="bge-small-en-v1.5"
            ;;
    esac
    
    print_info "Selected model: $ONNX_MODEL"
}

# Install system dependencies
install_system_deps() {
    if [ "$USE_CGO" -eq 0 ]; then
        return
    fi
    
    print_info "Installing system dependencies for ONNX support..."
    
    case "$OS" in
        linux)
            if command_exists apt-get; then
                sudo apt-get update
                sudo apt-get install -y build-essential pkg-config
            elif command_exists yum; then
                sudo yum groupinstall -y "Development Tools"
                sudo yum install -y pkg-config
            elif command_exists pacman; then
                sudo pacman -S --noconfirm base-devel pkg-config
            else
                print_warning "Could not detect package manager. Please install build-essential manually."
            fi
            ;;
        darwin)
            if ! command_exists brew; then
                print_warning "Homebrew not found. Skipping system dependencies."
                print_info "If build fails, install Xcode Command Line Tools:"
                print_info "  xcode-select --install"
            fi
            ;;
    esac
}

# Build and install tokenizers library
install_tokenizers() {
    if [ "$USE_CGO" -eq 0 ]; then
        return
    fi
    
    print_info "Building tokenizers library..."
    
    # Create temp directory
    TEMP_DIR=$(mktemp -d)
    cd "$TEMP_DIR"
    
    # Clone tokenizers - using daulet/tokenizers which provides Go bindings
    print_info "Cloning tokenizers repository..."
    git clone https://github.com/daulet/tokenizers.git --depth 1
    cd tokenizers
    
    # Build the tokenizers-ffi crate
    print_info "Compiling tokenizers (this may take a few minutes)..."
    cd crates/tokenizers
    cargo build --release
    cd ../..
    
    # Install library
    case "$OS" in
        linux)
            print_info "Installing tokenizers library to /usr/local/lib..."
            # Static library (.a) for CGO linking
            if [ -f "target/release/libtokenizers_ffi.a" ]; then
                sudo cp target/release/libtokenizers_ffi.a /usr/local/lib/libtokenizers.a
                print_success "Installed libtokenizers.a"
            fi
            # Shared library (.so) if available
            if [ -f "target/release/libtokenizers_ffi.so" ]; then
                sudo cp target/release/libtokenizers_ffi.so /usr/local/lib/libtokenizers.so
                sudo ldconfig
                print_success "Installed libtokenizers.so"
            fi
            # Header file
            if [ -f "crates/tokenizers-ffi/bindings.h" ]; then
                sudo cp crates/tokenizers-ffi/bindings.h /usr/local/include/tokenizers.h
                print_success "Installed tokenizers.h"
            fi
            print_success "Tokenizers library installed for Linux"
            ;;
        darwin)
            print_info "Installing tokenizers library to /usr/local/lib..."
            # Static library (.a) for CGO linking
            if [ -f "target/release/libtokenizers_ffi.a" ]; then
                sudo cp target/release/libtokenizers_ffi.a /usr/local/lib/libtokenizers.a
                print_success "Installed libtokenizers.a"
            fi
            # Dynamic library (.dylib) if available
            if [ -f "target/release/libtokenizers_ffi.dylib" ]; then
                sudo cp target/release/libtokenizers_ffi.dylib /usr/local/lib/libtokenizers.dylib
                print_success "Installed libtokenizers.dylib"
            fi
            # Header file
            if [ -f "crates/tokenizers-ffi/bindings.h" ]; then
                sudo cp crates/tokenizers-ffi/bindings.h /usr/local/include/tokenizers.h
                print_success "Installed tokenizers.h"
            fi
            print_success "Tokenizers library installed for macOS"
            ;;
        *)
            print_error "Unsupported OS for tokenizers installation"
            exit 1
            ;;
    esac
    
    # Cleanup
    rm -rf "$TEMP_DIR"
    cd "$SCRIPT_DIR"
}

# Install ONNX Runtime shared library
# Version must match onnxruntime_go — currently v1.27.0 requires onnxruntime 1.24.1
install_onnxruntime() {
    if [ "$USE_CGO" -eq 0 ]; then
        return
    fi

    ONNX_VERSION="1.24.1"

    # Determine the expected library file name for this platform
    case "$OS" in
        darwin)
            ONNX_LIB="/usr/local/lib/libonnxruntime.${ONNX_VERSION}.dylib"
            ONNX_LINK="/usr/local/lib/libonnxruntime.dylib"
            ONNX_SO_LINK="/usr/local/lib/onnxruntime.so"
            ;;
        linux)
            ONNX_LIB="/usr/local/lib/libonnxruntime.so.${ONNX_VERSION}"
            ONNX_LINK="/usr/local/lib/libonnxruntime.so"
            ONNX_SO_LINK="/usr/local/lib/onnxruntime.so"
            ;;
    esac

    # Skip if already installed
    if [ -f "$ONNX_LIB" ]; then
        print_success "ONNX Runtime ${ONNX_VERSION} already installed"
        return
    fi

    print_info "Installing ONNX Runtime ${ONNX_VERSION}..."

    # Map OS/ARCH to GitHub release asset name
    case "${OS}-${ARCH}" in
        darwin-arm64)
            ONNX_ARCHIVE="onnxruntime-osx-arm64-${ONNX_VERSION}.tgz"
            ONNX_LIB_IN_ARCHIVE="onnxruntime-osx-arm64-${ONNX_VERSION}/lib/libonnxruntime.${ONNX_VERSION}.dylib"
            ;;
        darwin-amd64)
            ONNX_ARCHIVE="onnxruntime-osx-x86_64-${ONNX_VERSION}.tgz"
            ONNX_LIB_IN_ARCHIVE="onnxruntime-osx-x86_64-${ONNX_VERSION}/lib/libonnxruntime.${ONNX_VERSION}.dylib"
            ;;
        linux-arm64)
            ONNX_ARCHIVE="onnxruntime-linux-aarch64-${ONNX_VERSION}.tgz"
            ONNX_LIB_IN_ARCHIVE="onnxruntime-linux-aarch64-${ONNX_VERSION}/lib/libonnxruntime.so.${ONNX_VERSION}"
            ;;
        linux-amd64)
            ONNX_ARCHIVE="onnxruntime-linux-x64-${ONNX_VERSION}.tgz"
            ONNX_LIB_IN_ARCHIVE="onnxruntime-linux-x64-${ONNX_VERSION}/lib/libonnxruntime.so.${ONNX_VERSION}"
            ;;
        *)
            print_warning "No pre-built ONNX Runtime for ${OS}/${ARCH}. Skipping."
            return
            ;;
    esac

    ONNX_URL="https://github.com/microsoft/onnxruntime/releases/download/v${ONNX_VERSION}/${ONNX_ARCHIVE}"

    # Download
    TEMP_DIR=$(mktemp -d)
    print_info "Downloading ${ONNX_ARCHIVE}..."
    if ! curl -fSL --progress-bar "$ONNX_URL" -o "${TEMP_DIR}/${ONNX_ARCHIVE}"; then
        print_error "Failed to download ONNX Runtime"
        rm -rf "$TEMP_DIR"
        return 1
    fi

    # Extract
    print_info "Extracting..."
    tar -xzf "${TEMP_DIR}/${ONNX_ARCHIVE}" -C "$TEMP_DIR"

    # Install
    sudo cp "${TEMP_DIR}/${ONNX_LIB_IN_ARCHIVE}" "$ONNX_LIB"
    sudo ln -sf "$ONNX_LIB" "$ONNX_LINK"
    sudo ln -sf "$ONNX_LIB" "$ONNX_SO_LINK"

    # Linux: refresh linker cache
    if [ "$OS" = "linux" ]; then
        sudo ldconfig
    fi

    rm -rf "$TEMP_DIR"
    cd "$SCRIPT_DIR"

    print_success "ONNX Runtime ${ONNX_VERSION} installed for ${OS}/${ARCH}"
    print_info "  Library: $ONNX_LIB"
    print_info "  Symlink: $ONNX_LINK"
    print_info "  Symlink: $ONNX_SO_LINK"

    # Persist library path in shell profile on macOS
    if [ "$OS" = "darwin" ]; then
        SHELL_PROFILE=""
        if [ -f "$HOME/.zshrc" ]; then
            SHELL_PROFILE="$HOME/.zshrc"
        elif [ -f "$HOME/.bash_profile" ]; then
            SHELL_PROFILE="$HOME/.bash_profile"
        fi
        if [ -n "$SHELL_PROFILE" ]; then
            if ! grep -q "DYLD_LIBRARY_PATH.*usr/local/lib" "$SHELL_PROFILE" 2>/dev/null; then
                echo 'export DYLD_LIBRARY_PATH=/usr/local/lib:$DYLD_LIBRARY_PATH' >> "$SHELL_PROFILE"
                print_info "Added DYLD_LIBRARY_PATH to $SHELL_PROFILE"
            fi
        fi
        export DYLD_LIBRARY_PATH=/usr/local/lib:$DYLD_LIBRARY_PATH
    fi
}

# Build Agent DNS
build_agentdns() {
    print_info "Building Agent DNS..."
    
    cd "$SCRIPT_DIR"

    # Set CGO flag
    export CGO_ENABLED=$USE_CGO
    
    # Set library path for macOS
    if [ "$OS" = "darwin" ] && [ "$USE_CGO" -eq 1 ]; then
        export DYLD_LIBRARY_PATH=/usr/local/lib:$DYLD_LIBRARY_PATH
    fi
    
    # Build
    print_info "Compiling (CGO_ENABLED=$USE_CGO)..."
    go build -ldflags="-s -w" -o agentdns ./cmd/agentdns
    
    if [ $? -eq 0 ]; then
        print_success "Build successful"
    else
        print_error "Build failed"
        exit 1
    fi
}

# Install binary to system
install_binary() {
    print_info "Installing binary to system..."
    
    # Determine install location
    case "$OS" in
        linux|darwin)
            INSTALL_DIR="/usr/local/bin"
            BINARY_NAME="agentdns"
            ;;
        windows)
            INSTALL_DIR="$HOME/bin"
            BINARY_NAME="agentdns.exe"
            mkdir -p "$INSTALL_DIR"
            ;;
    esac
    
    # Remove old binary if exists
    if [ -f "$INSTALL_DIR/$BINARY_NAME" ]; then
        print_info "Removing old binary..."
        sudo rm -f "$INSTALL_DIR/$BINARY_NAME"
    fi
    
    # Install new binary
    print_info "Installing to $INSTALL_DIR/$BINARY_NAME..."
    sudo cp agentdns "$INSTALL_DIR/$BINARY_NAME"
    sudo chmod +x "$INSTALL_DIR/$BINARY_NAME"
    
    print_success "Binary installed to $INSTALL_DIR/$BINARY_NAME"
}

# Create default config
create_config() {
    print_info "Setting up configuration..."
    
    CONFIG_DIR="$HOME/.agentdns"
    mkdir -p "$CONFIG_DIR"
    
    if [ ! -f "$CONFIG_DIR/config.toml" ]; then
        print_info "Creating default config at $CONFIG_DIR/config.toml..."
        
        cat > "$CONFIG_DIR/config.toml" << EOF
# Agent DNS Configuration
# Generated by install script

[node]
name = "my-registry"
type = "full"
data_dir = "$CONFIG_DIR/data"
external_ip = "auto"

[mesh]
listen_port = 4001
max_peers = 15
bootstrap_peers = []

[gossip]
max_hops = 10
max_announcements_per_second = 100
dedup_window_seconds = 300

[registry]
postgres_url = "postgres://agentdns:agentdns@localhost:5432/agentdns?sslmode=disable"
max_local_agents = 100000

[search]
embedding_backend = "$BACKEND"
EOF

        if [ "$BACKEND" = "onnx" ]; then
            cat >> "$CONFIG_DIR/config.toml" << EOF
embedding_model = "$ONNX_MODEL"
embedding_model_dir = "$CONFIG_DIR/models"
EOF
        elif [ "$BACKEND" = "http" ]; then
            cat >> "$CONFIG_DIR/config.toml" << EOF
embedding_endpoint = "http://localhost:11434/api/embeddings"
EOF
        fi

        cat >> "$CONFIG_DIR/config.toml" << EOF
embedding_dimensions = 384
use_improved_keyword = true
max_federated_peers = 10
federated_timeout_ms = 1500
default_max_results = 20

[search.ranking]
text_relevance_weight = 0.30
semantic_similarity_weight = 0.30
trust_weight = 0.20
freshness_weight = 0.10
availability_weight = 0.10

[cache]
max_agent_cards = 50000
agent_card_ttl_seconds = 3600
max_gossip_entries = 2000000

[redis]
url = ""
prefix = "agdns:"

[trust]
min_display_score = 0.1
eigentrust_iterations = 5
attestation_gossip_interval_seconds = 3600

[api]
listen = "0.0.0.0:8080"
rate_limit_search = 100
rate_limit_register = 10
cors_origins = ["*"]

[bloom]
expected_tokens = 500000
false_positive_rate = 0.01
update_interval_seconds = 300
EOF

        print_success "Config created at $CONFIG_DIR/config.toml"
    else
        print_info "Config already exists at $CONFIG_DIR/config.toml"
    fi
}

# Verify installation
verify_installation() {
    print_info "Verifying installation..."
    
    if command_exists agentdns; then
        VERSION=$(agentdns version 2>/dev/null || echo "unknown")
        print_success "Agent DNS installed successfully!"
        print_info "Version: $VERSION"
    else
        print_error "Installation verification failed. Binary not found in PATH."
        exit 1
    fi
}

# Print next steps
print_next_steps() {
    echo ""
    echo -e "${GREEN}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║${NC}  Installation Complete!                                    ${GREEN}║${NC}"
    echo -e "${GREEN}╚════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    print_info "Configuration: $HOME/.agentdns/config.toml"
    print_info "Embedding Backend: $BACKEND"
    if [ "$BACKEND" = "onnx" ]; then
        print_info "ONNX Model: $ONNX_MODEL"
    fi
    echo ""
    echo -e "${BLUE}Next Steps:${NC}"
    echo ""
    echo "  1. Initialize your node:"
    echo -e "     ${YELLOW}agentdns init${NC}"
    echo ""
    
    if [ "$BACKEND" = "onnx" ]; then
        echo "  2. Download embedding model:"
        echo -e "     ${YELLOW}agentdns models download $ONNX_MODEL${NC}"
        echo ""
        echo "  3. Start the registry:"
    else
        echo "  2. Start the registry:"
    fi
    echo -e "     ${YELLOW}agentdns start${NC}"
    echo ""
    
    if [ "$BACKEND" = "http" ]; then
        echo -e "  ${YELLOW}Note:${NC} HTTP embedder requires an external service (e.g., Ollama)"
        echo "        Update embedding_endpoint in config.toml"
        echo ""
    fi
    
    echo "  For help:"
    echo -e "     ${YELLOW}agentdns help${NC}"
    echo ""
    echo "  Documentation:"
    echo "     - Quick Start: docs/QUICK_START_IMPROVED.md"
    echo "     - Improvements: docs/IMPROVEMENTS.md"
    echo "     - Build Guide: BUILD_GUIDE.md"
    echo ""
}

# Main installation flow
main() {
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
    print_header

    # System detection
    detect_system
    
    # Check prerequisites
    check_go
    
    # Choose embedding backend
    choose_embedding_backend
    
    # Choose ONNX model if needed
    choose_onnx_model
    
    # Install dependencies for ONNX
    if [ "$USE_CGO" -eq 1 ]; then
        # Check Rust
        if ! check_rust; then
            install_rust || {
                print_error "Rust installation failed or declined. Cannot build ONNX support."
                print_info "Falling back to Hash embedder..."
                BACKEND="hash"
                USE_CGO=0
            }
        fi
        
        if [ "$USE_CGO" -eq 1 ]; then
            install_system_deps
            install_tokenizers
            install_onnxruntime
        fi
    fi
    
    # Build Agent DNS
    build_agentdns
    
    # Install binary
    install_binary
    
    # Create config
    create_config
    
    # Verify
    verify_installation
    
    # Print next steps
    print_next_steps
}

# Run main
main
