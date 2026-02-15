#!/bin/bash
set -e

# Get the directory where this script is located
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "Using Project Root: $PROJECT_ROOT"

# Create resources directory if it doesn't exist
mkdir -p "$SCRIPT_DIR/resources"

echo "Building agent-server..."
cd "$PROJECT_ROOT/agent_go"
go build -o "$SCRIPT_DIR/resources/agent-server" .

echo "Building workspace-server..."
cd "$PROJECT_ROOT/workspace"
go build -o "$SCRIPT_DIR/resources/workspace-server" .

echo "Installing Electron dependencies..."
cd "$SCRIPT_DIR"
npm install

echo "Setup complete."
echo "To run the app: cd desktop && npm start"
