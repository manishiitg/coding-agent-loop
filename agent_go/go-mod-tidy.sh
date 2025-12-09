#!/bin/bash
# Script to run go mod tidy while excluding tool_output_folder
# This prevents errors from temporary generated code files

set -e

# Check if tool_output_folder exists and has .go files
if [ -d "tool_output_folder" ] && [ "$(find tool_output_folder -name '*.go' -type f | wc -l)" -gt 0 ]; then
    echo "⚠️  tool_output_folder contains .go files that may interfere with go mod tidy"
    echo "📦 Running go mod tidy with temporary exclusion..."
    
    # Create a temporary directory to move tool_output_folder
    TEMP_DIR=$(mktemp -d)
    TEMP_TOOL_OUTPUT="${TEMP_DIR}/tool_output_folder"
    
    # Move tool_output_folder temporarily
    if [ -d "tool_output_folder" ]; then
        mv tool_output_folder "$TEMP_TOOL_OUTPUT"
        echo "📁 Temporarily moved tool_output_folder to ${TEMP_DIR}"
    fi
    
    # Run go mod tidy
    go mod tidy
    
    # Restore tool_output_folder
    if [ -d "$TEMP_TOOL_OUTPUT" ]; then
        mv "$TEMP_TOOL_OUTPUT" tool_output_folder
        echo "✅ Restored tool_output_folder"
    fi
    
    # Cleanup temp directory
    rmdir "$TEMP_DIR" 2>/dev/null || true
else
    # No tool_output_folder or no .go files, run normally
    go mod tidy
fi

echo "✅ go mod tidy completed successfully"
