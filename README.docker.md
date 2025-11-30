# Docker Setup

Simple docker-compose setup for MCP Agent and Frontend.

## Quick Start

```bash
# Start all services
docker-compose up -d

# View logs
docker-compose logs -f

# Stop services
docker-compose down
```

## Services

- **MCP Agent** (8000) - Main server
- **Frontend** (5173) - React app
- **Workspace API** (8081) - File management
- **Qdrant** (6333) - Vector database

## URLs

- Frontend: http://localhost:5173
- MCP Agent: http://localhost:8000
- Workspace API: http://localhost:8081
- Qdrant: http://localhost:6333

## Obsidian REST API Setup

To enable the REST API plugin in Obsidian:

1. Open http://localhost:27124 in your browser
2. Go to Settings → Community plugins
3. Turn off Safe mode
4. Browse available plugins and search for "REST API"
5. Install and enable the REST API plugin
6. Configure the plugin with your desired settings
7. The REST API will be available on port 27123
