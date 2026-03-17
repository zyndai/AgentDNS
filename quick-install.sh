#!/bin/bash

# Quick Install - Installs Agent DNS with ONNX embedder (recommended defaults)

set -e

echo "╔════════════════════════════════════════════════════════════╗"
echo "║  Agent DNS Quick Install (ONNX + bge-small-en-v1.5)       ║"
echo "╚════════════════════════════════════════════════════════════╝"
echo ""

# Non-interactive defaults
export EMBEDDING_CHOICE=2         # ONNX embedder
export MODEL_CHOICE=2             # bge-small-en-v1.5 (recommended)
export NONINTERACTIVE=1

# Run main install script
./install.sh
