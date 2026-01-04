# Git Conflict Resolution Guide

## Quick Start

### 1. Check for Conflicts
```bash
cd /app/workspace-docs  # or your DOCS_DIR
git status
```

### 2. View Conflicted Files
```bash
# List all conflicted files
git diff --name-only --diff-filter=U

# Or check status
git status --porcelain | grep "^UU\|^AA\|^DU\|^UD"
```

### 3. Resolve Each File

Open each conflicted file and look for conflict markers:
```
<<<<<<< HEAD
Your local changes (current branch)
=======
Changes from GitHub (incoming changes)
>>>>>>> origin/main
```

**Edit the file to:**
- Keep your version: Remove markers, keep your content
- Keep remote version: Remove markers, keep remote content  
- Merge both: Combine content manually, remove markers
- Write new content: Replace everything

### 4. Mark as Resolved
```bash
# Stage each resolved file
git add <file-path>

# Example:
git add docs/example.md
```

### 5. Complete Merge
```bash
# Commit the resolution
git commit -m "Resolve merge conflicts"

# Push to GitHub
git push origin main
```

## Alternative: Use Force Operations

### Force Push Local (Overwrite GitHub)
```bash
# Via API
curl -X POST http://localhost:8081/api/sync/github \
  -H "Content-Type: application/json" \
  -d '{"operation": "force_push_local", "commit_message": "Keep local changes"}'

# Via Git
git push --force origin main
```

### Force Pull Remote (Overwrite Local)
```bash
# Via API
curl -X POST http://localhost:8081/api/sync/github \
  -H "Content-Type: application/json" \
  -d '{"operation": "force_pull_remote"}'

# Via Git
git fetch origin
git reset --hard origin/main
git clean -fd
```

## Docker Deployment

### Method 1: Resolve Conflicts Inside Docker Container

If conflicts occur during Docker deployment, you can resolve them directly inside the container:

```bash
# 1. Find the container name
docker ps | grep workspace-api

# 2. Access the container (container name is usually: mcp-agent-builder-go-workspace-api-1)
docker exec -it mcp-agent-builder-go-workspace-api-1 /bin/sh

# Or use docker-compose:
docker-compose exec workspace-api /bin/sh

# 3. Navigate to workspace docs directory
cd /app/workspace-docs

# 4. Check git status and conflicts
git status

# 5. View conflicted files
git diff --name-only --diff-filter=U

# 6. Edit conflicted files using vi or nano
# Install editor if needed (Alpine Linux):
# apk add --no-cache nano vim

# Edit each conflicted file:
vi <conflicted-file-path>
# or
nano <conflicted-file-path>

# Remove conflict markers (<<<<<<< HEAD, =======, >>>>>>> origin/main)
# Keep the content you want, then save

# 7. Mark files as resolved
git add <resolved-file-path>

# Or stage all resolved files:
git add .

# 8. Complete the merge
git commit -m "Resolve merge conflicts"

# 9. Push to GitHub
git push origin main

# 10. Exit container
exit

# 11. Restart the container (if server failed to start)
docker-compose restart workspace-api
```

### Method 2: Resolve Conflicts from Host (Easier)

Since `workspace-docs` is mounted as a volume (`./workspace-docs:/app/workspace-docs`), you can resolve conflicts from your host machine:

```bash
# 1. Navigate to workspace-docs on host
cd /path/to/mcp-agent-builder-go/workspace-docs

# 2. Check conflicts
git status

# 3. Edit conflicted files with your favorite editor
# (VS Code, vim, nano, etc.)
code .  # Opens in VS Code
# or
vim <conflicted-file>

# 4. Resolve conflicts (remove markers, keep desired content)

# 5. Stage resolved files
git add <resolved-file>

# 6. Complete merge
git commit -m "Resolve merge conflicts"

# 7. Push to GitHub
git push origin main

# 8. Restart Docker container
cd /path/to/mcp-agent-builder-go
docker-compose restart workspace-api
```

### Method 3: Use Force Operations via API

If the server is running but conflicts exist:

```bash
# Check sync status
curl http://localhost:8081/api/sync/status | jq .

# Option A: Force push local changes (overwrite GitHub)
curl -X POST http://localhost:8081/api/sync/github \
  -H "Content-Type: application/json" \
  -d '{
    "operation": "force_push_local",
    "commit_message": "Resolve conflicts by keeping local changes"
  }'

# Option B: Force pull remote changes (overwrite local)
curl -X POST http://localhost:8081/api/sync/github \
  -H "Content-Type: application/json" \
  -d '{
    "operation": "force_pull_remote"
  }'
```

### Method 4: Use Force Operations Inside Container

```bash
# Access container
docker-compose exec workspace-api /bin/sh

# Navigate to docs
cd /app/workspace-docs

# Force push local (overwrite GitHub)
git push --force origin main

# OR Force pull remote (overwrite local)
git fetch origin
git reset --hard origin/main
git clean -fd

# Exit
exit

# Restart container
docker-compose restart workspace-api
```

### Quick Reference: Docker Commands

```bash
# Find container name
docker ps | grep workspace

# Access container shell
docker-compose exec workspace-api /bin/sh

# View container logs
docker-compose logs workspace-api

# Restart container
docker-compose restart workspace-api

# Stop container
docker-compose stop workspace-api

# Start container
docker-compose start workspace-api

# Rebuild and restart
docker-compose up -d --build workspace-api
```

## Conflict Status Codes

- `UU` - Both modified (conflict)
- `AA` - Both added (conflict)
- `DU` - Deleted locally, modified remotely
- `UD` - Modified locally, deleted remotely

## Tips

1. **Always backup** before force operations
2. **Review conflicts carefully** - don't blindly accept one side
3. **Test after resolution** - ensure files work correctly
4. **Use descriptive commit messages** - document what was resolved
5. **Check git log** - understand what caused the conflict

## API Endpoints

- `GET /api/sync/status` - Check sync status and conflicts
- `POST /api/sync/github` - Sync with options:
  - `operation: "sync"` - Normal sync (fails on conflicts)
  - `operation: "force_push_local"` - Overwrite GitHub
  - `operation: "force_pull_remote"` - Overwrite local

