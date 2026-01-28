#!/bin/bash
# Build and push workspace-api image to Docker Hub (manishiitg/workspace-api)
# Run from anywhere; uses workspace/ as build context.
# Prerequisites: docker login first (docker login -u manishiitg)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE="${DOCKER_IMAGE:-manishiitg/workspace-api:latest}"

echo "Building workspace-api from $SCRIPT_DIR"
docker build -t "$IMAGE" -f "$SCRIPT_DIR/Dockerfile" "$SCRIPT_DIR"

echo "Pushing $IMAGE"
docker push "$IMAGE"

echo "Done. Image pushed: $IMAGE"
