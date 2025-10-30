#!/bin/bash
for file in $(find pkg/orchestrator/agents -name "*.go"); do
    sed -i 's/eventBridge interface{}/eventBridge EventBridge/g' "$file"
done
