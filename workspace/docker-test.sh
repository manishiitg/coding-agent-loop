#!/bin/bash

# Docker test script for Planner API

echo "🐳 Testing Planner API with Docker..."

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo "❌ Docker is not running. Please start Docker first."
    exit 1
fi

# Check if .env file exists
if [ ! -f .env ]; then
    echo "📝 Creating .env file from example..."
    cp env.example .env
    echo "⚠️  Please edit .env file with your GitHub token and repository"
    echo "   GITHUB_TOKEN=ghp_your_token_here"
    echo "   GITHUB_REPO=your-username/your-repo"
    exit 1
fi

# Build and start services
echo "🔨 Building and starting services..."
docker-compose up --build -d

# Wait for services to be ready
echo "⏳ Waiting for services to be ready..."
sleep 10

# Test health endpoint
echo "🏥 Testing health endpoint..."
curl -s http://localhost:8080/health | jq .

# Test create document
echo "📝 Testing create document..."
curl -s -X POST http://localhost:8080/api/documents \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Docker Test Document",
    "content": "# Docker Test\n\nThis document was created via Docker!",
    "folder": "docker-tests"
  }' | jq .

# Test list documents
echo "📋 Testing list documents..."
curl -s http://localhost:8080/api/documents | jq .

# Test search with ripgrep
echo "🔍 Testing search with ripgrep..."
curl -s "http://localhost:8080/api/documents/search?query=docker&search_type=all" | jq .

# Test structure analysis
echo "📊 Testing structure analysis..."
curl -s "http://localhost:8080/api/documents/docker-test-document/structure" | jq .

# Test GitHub sync status
echo "🔄 Testing GitHub sync status..."
curl -s "http://localhost:8080/api/sync/status" | jq .

# Show container logs
echo "📋 Container logs:"
docker-compose logs --tail=20 planner-api

# Show file locations
echo "📁 File locations in container:"
docker-compose exec planner-api ls -la /app/workspace-docs/

echo "✅ Docker test completed!"
echo "🌐 API available at: http://localhost:8080"
echo "📊 Health check: http://localhost:8080/health"
echo "📚 API docs: http://localhost:8080/api/documents"

# Ask if user wants to stop services
read -p "🛑 Stop services? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "🛑 Stopping services..."
    docker-compose down
else
    echo "▶️  Services are still running. Use 'docker-compose down' to stop them."
fi
