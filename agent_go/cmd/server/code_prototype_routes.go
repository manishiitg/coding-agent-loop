package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// Data types
// ---------------------------------------------------------------------------

type PrototypeProjectMeta struct {
	Name        string             `json:"name"`
	Type        string             `json:"type"` // frontend-only | backend-only | fullstack
	Description string             `json:"description,omitempty"`
	CreatedAt   string             `json:"created_at"`
	Config      PrototypeConfig    `json:"config"`
	Deployments []DeploymentRecord `json:"deployments,omitempty"`
	GitHub      *PrototypeGitHub   `json:"github,omitempty"`
}

// PrototypeGitHub holds GitHub connection info persisted in .prototype.json.
// The PAT is stored encrypted in the user secrets DB; only the secret name is here.
type PrototypeGitHub struct {
	RepoURL       string `json:"repo_url"`
	PatSecretName string `json:"pat_secret_name"`
}

type PrototypeConfig struct {
	SelectedServers   []string               `json:"selected_servers,omitempty"`
	SelectedSecrets   []string               `json:"selected_secrets,omitempty"`
	SelectedSkills    []string               `json:"selected_skills,omitempty"`
	SelectedSubAgents []string               `json:"selected_subagents,omitempty"`
	LLMConfig         map[string]interface{} `json:"llm_config,omitempty"`
}

type DeploymentRecord struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"` // k8s | vercel | railway
	URL       string `json:"url"`
	Timestamp string `json:"timestamp"`
	Status    string `json:"status"` // success | failed
	Logs      string `json:"logs,omitempty"`
}

// ---------------------------------------------------------------------------
// Scaffold templates (Go string constants)
// ---------------------------------------------------------------------------

// Frontend scaffold — React 19 + Vite 7 + TypeScript 5.9 + Tailwind v4 + shadcn/ui (2026 best practices)
const scaffoldFrontendPackageJSON = `{
  "name": "{{PROJECT_NAME}}-frontend",
  "version": "0.0.1",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -p tsconfig.app.json && vite build",
    "preview": "vite preview"
  },
  "dependencies": {
    "class-variance-authority": "^0.7.1",
    "clsx": "^2.1.1",
    "lucide-react": "^0.511.0",
    "react": "^19.2.0",
    "react-dom": "^19.2.0",
    "tailwind-merge": "^3.3.0"
  },
  "devDependencies": {
    "@tailwindcss/vite": "^4.1.0",
    "@types/react": "^19.0.0",
    "@types/react-dom": "^19.0.0",
    "@vitejs/plugin-react-swc": "^3.10.0",
    "tailwindcss": "^4.1.0",
    "typescript": "~5.9.0",
    "vite": "^7.0.0"
  }
}
`

const scaffoldViteConfig = `import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react-swc'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath } from 'node:url'
import fs from 'node:fs'

// Writes the actual bound port to .devport so the Go preview proxy can find it
// even when Vite bumps the port because the configured one is in use.
const writeDevPort = {
  name: 'write-devport',
  configureServer(server: any) {
    server.httpServer?.once('listening', () => {
      const port = (server.httpServer?.address() as any)?.port
      if (port) fs.writeFileSync('.devport', String(port))
    })
  },
}

export default defineConfig({
  plugins: [react(), tailwindcss(), writeDevPort],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  // In dev mode, VITE_BASE is set automatically by the dev command in .prototype.json
  // so assets are served through the Go preview proxy at /api/code-prototype/preview/{name}/
  // For production K8s deploys, the build step sets VITE_BASE to /prototypes/{name}/
  base: process.env.VITE_BASE ?? '/',
  server: {
    port: {{DEV_PORT}},
    host: '0.0.0.0',
  },
})
`

// Root tsconfig — project references only (Vite 7 official template structure)
const scaffoldFrontendTsConfig = `{
  "files": [],
  "references": [
    { "path": "./tsconfig.app.json" },
    { "path": "./tsconfig.node.json" }
  ]
}
`

// tsconfig.app.json — for src/ browser code
const scaffoldFrontendTsConfigApp = `{
  "compilerOptions": {
    "tsBuildInfoFile": "./node_modules/.tmp/tsconfig.app.tsbuildinfo",
    "target": "ES2023",
    "useDefineForClassFields": true,
    "lib": ["ES2023", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "types": ["vite/client"],
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "verbatimModuleSyntax": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "erasableSyntaxOnly": true,
    "noFallthroughCasesInSwitch": true,
    "noUncheckedSideEffectImports": true,
    "baseUrl": ".",
    "paths": {
      "@/*": ["./src/*"]
    }
  },
  "include": ["src"]
}
`

// tsconfig.node.json — for vite.config.ts (runs in Node, not the browser)
const scaffoldFrontendTsConfigNode = `{
  "compilerOptions": {
    "tsBuildInfoFile": "./node_modules/.tmp/tsconfig.node.tsbuildinfo",
    "target": "ES2023",
    "lib": ["ES2023"],
    "module": "ESNext",
    "moduleResolution": "bundler",
    "skipLibCheck": true,
    "noEmit": true,
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "erasableSyntaxOnly": true
  },
  "include": ["vite.config.ts"]
}
`

const scaffoldIndexHTML = `<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>{{PROJECT_NAME}}</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
`

const scaffoldMainTsx = `import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './index.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
)
`

const scaffoldAppTsx = `import React, { useState } from 'react'

export default function App() {
  const [count, setCount] = useState(0)

  return (
    <div className="min-h-screen bg-white flex items-center justify-center">
      <div className="text-center space-y-4">
        <h1 className="text-3xl font-bold text-slate-900">{{PROJECT_NAME}}</h1>
        <p className="text-slate-500">Count: {count}</p>
        <button
          onClick={() => setCount(c => c + 1)}
          className="px-4 py-2 bg-slate-900 text-white rounded-md hover:bg-slate-700 transition-colors"
        >
          Increment
        </button>
      </div>
    </div>
  )
}
`

// Tailwind v4 global CSS — @import replaces the old @tailwind directives
const scaffoldIndexCSS = `@import "tailwindcss";
`

// shadcn/ui cn utility — merges Tailwind classes safely
const scaffoldLibUtils = `import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
`

// components.json — tells the shadcn CLI where to put components and how the project is configured.
// Run: npx shadcn@latest add <component>  (e.g. button, card, input, dialog)
const scaffoldComponentsJSON = `{
  "$schema": "https://ui.shadcn.com/schema.json",
  "style": "default",
  "rsc": false,
  "tsx": true,
  "tailwind": {
    "config": "",
    "css": "src/index.css",
    "baseColor": "slate",
    "cssVariables": true,
    "prefix": ""
  },
  "aliases": {
    "components": "@/components",
    "utils": "@/lib/utils",
    "ui": "@/components/ui",
    "lib": "@/lib",
    "hooks": "@/hooks"
  },
  "iconLibrary": "lucide"
}
`

// Backend scaffold — Express 5 + tsx + TypeScript 5.9 + CommonJS (2026 best practices)
const scaffoldBackendPackageJSON = `{
  "name": "{{PROJECT_NAME}}-backend",
  "version": "0.0.1",
  "private": true,
  "scripts": {
    "dev": "tsx watch src/index.ts",
    "build": "tsc",
    "start": "node dist/index.js"
  },
  "dependencies": {
    "cors": "^2.8.5",
    "express": "^5.1.0"
  },
  "devDependencies": {
    "@types/cors": "^2.8.17",
    "@types/express": "^5.0.0",
    "@types/node": "^22.0.0",
    "tsx": "^4.20.0",
    "typescript": "~5.9.0"
  }
}
`

const scaffoldBackendTsConfig = `{
  "compilerOptions": {
    "target": "ES2023",
    "module": "commonjs",
    "lib": ["ES2023"],
    "outDir": "dist",
    "rootDir": "src",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "sourceMap": true
  },
  "include": ["src"],
  "exclude": ["node_modules", "dist"]
}
`

// Express 5: async errors are automatically forwarded to error middleware — no try/catch needed.
const scaffoldIndexTs = `import express from 'express'
import cors from 'cors'

const app = express()
const PORT = process.env.PORT || 3000

app.use(cors())
app.use(express.json())

app.get('/health', (_req, res) => {
  res.json({ status: 'ok' })
})

app.listen(PORT, () => {
  console.log('Server running on port ' + PORT)
})
`

// Root package.json for fullstack projects — uses concurrently to start both dev servers in one command.
// VITE_BASE is injected so the Vite dev server serves assets through the Go preview proxy.
const scaffoldGitIgnore = `# Dependencies
node_modules/
.pnp
.pnp.js

# Build output
dist/
build/
*.tsbuildinfo

# Environment & secrets
.env
.env.local
.env.*.local

# Logs
*.log
dev.log
npm-debug.log*

# OS / editor
.DS_Store
Thumbs.db
.idea/
.vscode/
*.swp

# Coverage
coverage/
.nyc_output/
`

// Root package.json for fullstack projects — uses concurrently to start both dev servers in one command.
// VITE_BASE is injected so the Vite dev server serves assets through the Go preview proxy.
// stdout+stderr are appended to dev.log so the agent can inspect them for errors.
const scaffoldRootPackageJSON = `{
  "name": "{{PROJECT_NAME}}",
  "private": true,
  "scripts": {
    "dev": "npm run stop; concurrently --no-color --names \"frontend,backend\" \"VITE_BASE={{PREVIEW_BASE}} npm run dev --prefix frontend\" \"npm run dev --prefix backend\" > dev.log 2>&1",
    "stop": "pkill -f 'vite' 2>/dev/null; pkill -f 'tsx' 2>/dev/null; rm -f frontend/.devport; echo 'Dev servers stopped'",
    "install:all": "npm install --prefix frontend && npm install --prefix backend && npm install"
  },
  "devDependencies": {
    "concurrently": "^9.0.0"
  }
}
`

// ---------------------------------------------------------------------------
// Helper: write file to workspace via REST
// ---------------------------------------------------------------------------

func writePrototypeFile(ctx context.Context, filePath, content, userID string) error {
	pathSegments := strings.Split(filePath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, seg := range pathSegments {
		encodedSegments[i] = url.PathEscape(seg)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	body, _ := json.Marshal(map[string]string{"content": content})
	apiURL := getProjectsAPIURL() + "/api/documents/" + encodedPath

	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("workspace API %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helper: read file from workspace
// ---------------------------------------------------------------------------

func readPrototypeFile(ctx context.Context, filePath, userID string) (string, error) {
	pathSegments := strings.Split(filePath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, seg := range pathSegments {
		encodedSegments[i] = url.PathEscape(seg)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	apiURL := getProjectsAPIURL() + "/api/documents/" + encodedPath + "/raw"
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", err
	}
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("workspace API %d: %s", resp.StatusCode, string(b))
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

// ---------------------------------------------------------------------------
// Helper: delete workspace folder via REST
// ---------------------------------------------------------------------------

func deletePrototypeFolder(ctx context.Context, folderPath, userID string) error {
	pathSegments := strings.Split(folderPath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, seg := range pathSegments {
		encodedSegments[i] = url.PathEscape(seg)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	apiURL := getProjectsAPIURL() + "/api/folders/" + encodedPath + "?confirm=true"
	req, err := http.NewRequestWithContext(ctx, "DELETE", apiURL, nil)
	if err != nil {
		return err
	}
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("workspace API %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helper: prototype folder path
// ---------------------------------------------------------------------------

func prototypeFolderPath(_, projectName string) string {
	return "Projects/" + projectName
}

func prototypeMetaPath(userID, projectName string) string {
	return prototypeFolderPath(userID, projectName) + "/.prototype.json"
}

// ---------------------------------------------------------------------------
// Helper: scaffold project files
// ---------------------------------------------------------------------------

func scaffoldPrototypeFiles(ctx context.Context, userID string, meta PrototypeProjectMeta) error {
	name := meta.Name
	base := prototypeFolderPath(userID, name)

	previewBase := "/api/code-prototype/preview/" + name + "/"
	replacer := strings.NewReplacer(
		"{{PROJECT_NAME}}", name,
		"{{DEV_PORT}}", strconv.Itoa(projectDevPort(name)),
		"{{PREVIEW_BASE}}", previewBase,
	)

	files := map[string]string{
		base + "/.gitignore": scaffoldGitIgnore,
	}

	// Fullstack: root package.json with concurrently so `npm run dev` starts both servers
	if meta.Type == "fullstack" {
		files[base+"/package.json"] = replacer.Replace(scaffoldRootPackageJSON)
	}

	if meta.Type == "frontend-only" || meta.Type == "fullstack" {
		files[base+"/frontend/package.json"] = replacer.Replace(scaffoldFrontendPackageJSON)
		files[base+"/frontend/vite.config.ts"] = replacer.Replace(scaffoldViteConfig)
		files[base+"/frontend/tsconfig.json"] = replacer.Replace(scaffoldFrontendTsConfig)
		files[base+"/frontend/tsconfig.app.json"] = replacer.Replace(scaffoldFrontendTsConfigApp)
		files[base+"/frontend/tsconfig.node.json"] = replacer.Replace(scaffoldFrontendTsConfigNode)
		files[base+"/frontend/index.html"] = replacer.Replace(scaffoldIndexHTML)
		files[base+"/frontend/src/main.tsx"] = replacer.Replace(scaffoldMainTsx)
		files[base+"/frontend/src/App.tsx"] = replacer.Replace(scaffoldAppTsx)
		files[base+"/frontend/src/index.css"] = scaffoldIndexCSS
		files[base+"/frontend/src/lib/utils.ts"] = scaffoldLibUtils
		files[base+"/frontend/components.json"] = scaffoldComponentsJSON
	}

	if meta.Type == "backend-only" || meta.Type == "fullstack" {
		files[base+"/backend/package.json"] = replacer.Replace(scaffoldBackendPackageJSON)
		files[base+"/backend/tsconfig.json"] = replacer.Replace(scaffoldBackendTsConfig)
		files[base+"/backend/src/index.ts"] = replacer.Replace(scaffoldIndexTs)
	}

	for path, content := range files {
		if err := writePrototypeFile(ctx, path, content, userID); err != nil {
			return fmt.Errorf("scaffold %s: %w", path, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Route handlers
// ---------------------------------------------------------------------------

// GET /api/code-prototype/projects
func (api *StreamingAPI) handleListPrototypeProjects(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// List subfolders of Projects/ via workspace-projects-api (X-User-ID scopes to this user)
	wsURL := getProjectsAPIURL() + "/api/documents?folder=Projects"
	req, err := http.NewRequestWithContext(r.Context(), "GET", wsURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("X-User-ID", userID)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Workspace API returns {"success": true, "data": [{"filepath": "...", "type": "folder"|"file", ...}]}
	var dirResp struct {
		Data []struct {
			FilePath string `json:"filepath"`
			Type     string `json:"type"`
		} `json:"data"`
	}
	if resp.StatusCode == http.StatusOK {
		json.NewDecoder(resp.Body).Decode(&dirResp)
	}
	log.Printf("[CODE-PROTOTYPE] listProjects: workspace returned %d items for user %s", len(dirResp.Data), userID)

	projects := []PrototypeProjectMeta{}
	for _, f := range dirResp.Data {
		if f.Type != "folder" {
			continue
		}
		// filepath is e.g. "Projects/my-app" — extract just the project name
		parts := strings.Split(strings.TrimSuffix(f.FilePath, "/"), "/")
		projectName := parts[len(parts)-1]
		if projectName == "" || projectName == "Projects" {
			continue
		}
		metaPath := prototypeMetaPath(userID, projectName)
		content, _ := readPrototypeFile(r.Context(), metaPath, userID)
		if content == "" {
			continue
		}
		var meta PrototypeProjectMeta
		if err := json.Unmarshal([]byte(content), &meta); err == nil {
			projects = append(projects, meta)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projects)
}

// POST /api/code-prototype/projects
func (api *StreamingAPI) handleCreatePrototypeProject(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name        string         `json:"name"`
		Type        string         `json:"type"`
		Description string         `json:"description,omitempty"`
		Config      PrototypeConfig `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Type == "" {
		http.Error(w, "name and type are required", http.StatusBadRequest)
		return
	}

	meta := PrototypeProjectMeta{
		Name:        req.Name,
		Type:        req.Type,
		Description: req.Description,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Config:      req.Config,
	}

	// Write .prototype.json
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	metaPath := prototypeMetaPath(userID, req.Name)
	if err := writePrototypeFile(r.Context(), metaPath, string(metaJSON), userID); err != nil {
		log.Printf("[CODE-PROTOTYPE] Failed to write meta: %v", err)
		http.Error(w, "failed to create project", http.StatusInternalServerError)
		return
	}

	// Scaffold files
	if err := scaffoldPrototypeFiles(r.Context(), userID, meta); err != nil {
		log.Printf("[CODE-PROTOTYPE] Scaffold error: %v", err)
		// Non-fatal: meta was written, scaffold partially failed
	}

	// Initialize a git repo immediately so git commands are always scoped to this
	// project folder. Without this, git walks up the workspace tree and finds the
	// workspace root's own .git, producing completely wrong status/log output.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		initCmd := `git init -b main 2>/dev/null || git init && ` +
			`git config --local user.email "prototype@mcpagent.io" && ` +
			`git config --local user.name "MCP Agent"`
		if out, err := runProjectGit(ctx, userID, req.Name, initCmd); err != nil {
			log.Printf("[CODE-PROTOTYPE] git init error for %s: %v — %s", req.Name, err, out)
		} else {
			gitAutoSave(ctx, userID, req.Name, "Initial scaffold")
			log.Printf("[CODE-PROTOTYPE] git init done for %s", req.Name)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(meta)
}

// GET /api/code-prototype/projects/{name}
func (api *StreamingAPI) handleGetPrototypeProject(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	name := mux.Vars(r)["name"]
	content, err := readPrototypeFile(r.Context(), prototypeMetaPath(userID, name), userID)
	if err != nil || content == "" {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	var meta PrototypeProjectMeta
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		http.Error(w, "invalid project metadata", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(meta)
}

// PATCH /api/code-prototype/projects/{name}/config
func (api *StreamingAPI) handleUpdatePrototypeConfig(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	name := mux.Vars(r)["name"]
	var newConfig PrototypeConfig
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Read existing meta
	content, err := readPrototypeFile(r.Context(), prototypeMetaPath(userID, name), userID)
	if err != nil || content == "" {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	var meta PrototypeProjectMeta
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		http.Error(w, "invalid project metadata", http.StatusInternalServerError)
		return
	}

	meta.Config = newConfig
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	if err := writePrototypeFile(r.Context(), prototypeMetaPath(userID, name), string(metaJSON), userID); err != nil {
		http.Error(w, "failed to save config", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(meta)
}

// DELETE /api/code-prototype/projects/{name}
func (api *StreamingAPI) handleDeletePrototypeProject(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	name := mux.Vars(r)["name"]
	folder := prototypeFolderPath(userID, name)
	if err := deletePrototypeFolder(r.Context(), folder, userID); err != nil {
		log.Printf("[CODE-PROTOTYPE] Delete error: %v", err)
		http.Error(w, "failed to delete project", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// POST /api/code-prototype/deploy
func (api *StreamingAPI) handleDeployPrototype(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		ProjectName string `json:"project_name"`
		Provider    string `json:"provider"` // k8s | vercel | railway
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Read project meta
	content, err := readPrototypeFile(r.Context(), prototypeMetaPath(userID, req.ProjectName), userID)
	if err != nil || content == "" {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	var meta PrototypeProjectMeta
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		http.Error(w, "invalid project metadata", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	var deployURL, logs string
	var deployErr error

	switch req.Provider {
	case "k8s", "":
		deployURL, logs, deployErr = deployToK8s(ctx, userID, meta)
	case "vercel":
		deployURL, logs, deployErr = deployToVercel(ctx, userID, meta)
	case "railway":
		deployURL, logs, deployErr = deployToRailway(ctx, userID, meta)
	default:
		http.Error(w, "unknown provider: "+req.Provider, http.StatusBadRequest)
		return
	}

	status := "success"
	if deployErr != nil {
		status = "failed"
		log.Printf("[CODE-PROTOTYPE] Deploy error (%s): %v", req.Provider, deployErr)
	}

	// Append deployment record to .prototype.json
	deployID := fmt.Sprintf("dpl_%d", time.Now().UnixMilli())
	record := DeploymentRecord{
		ID:        deployID,
		Provider:  req.Provider,
		URL:       deployURL,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Status:    status,
		Logs:      logs,
	}
	meta.Deployments = append(meta.Deployments, record)
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	_ = writePrototypeFile(context.Background(), prototypeMetaPath(userID, req.ProjectName), string(metaJSON), userID)

	if deployErr != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": deployErr.Error(),
			"logs":  logs,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"url":           deployURL,
		"logs":          logs,
		"deployment_id": deployID,
	})
}

// POST /api/code-prototype/stop-dev
// Stops and removes all per-project Docker containers (and their node_modules volumes)
// for the current user, then clears the Go-side start-lock map.
func (api *StreamingAPI) handleStopDevServers(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Find all prototype containers for this user (docker filter is a substring match,
	// so we do an additional HasPrefix check to avoid false positives).
	filterPrefix := "prototype-" + userID + "-"
	listOut, _ := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "name="+filterPrefix,
		"--format", "{{.Names}}").Output()

	var stopped []string
	for _, name := range strings.Fields(strings.TrimSpace(string(listOut))) {
		if !strings.HasPrefix(name, filterPrefix) {
			continue
		}
		// Remove the container but keep node_modules named volumes so the next
		// start skips npm install and boots immediately.
		exec.CommandContext(ctx, "docker", "rm", "-f", name).Run() //nolint:errcheck
		stopped = append(stopped, name)
	}

	// Clear container start-locks for this user.
	prefix := userID + "/"
	containerStarting.Range(func(k, _ interface{}) bool {
		if key, ok := k.(string); ok && strings.HasPrefix(key, prefix) {
			containerStarting.Delete(key)
		}
		return true
	})

	log.Printf("[CODE-PROTOTYPE] stop-dev for user %s: stopped %v", userID, stopped)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"message": "dev servers stopped", "stopped": stopped})
}

// DELETE /api/code-prototype/deploy/{projectName}
func (api *StreamingAPI) handleUndeployPrototype(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	projectName := mux.Vars(r)["projectName"]
	logs, err := undeployFromK8s(r.Context(), userID, projectName)
	if err != nil {
		log.Printf("[CODE-PROTOTYPE] Undeploy error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "logs": logs})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Deployer: K8s
// ---------------------------------------------------------------------------

func deployToK8s(ctx context.Context, userID string, meta PrototypeProjectMeta) (string, string, error) {
	var logBuilder strings.Builder
	wsURL := getProjectsAPIURL()
	baseDir := "/app/workspace-projects/_users/" + userID + "/Projects/" + meta.Name
	namespace := getK8sNamespace()
	ingressHost := getIngressHost()

	// 1. Build step
	if meta.Type == "frontend-only" || meta.Type == "fullstack" {
		buildCmd := "cd " + baseDir + "/frontend && npm install && npm run build"
		out, err := runWorkspaceShell(ctx, wsURL, userID, buildCmd, ".")
		logBuilder.WriteString("=== frontend build ===\n" + out + "\n")
		if err != nil {
			return "", logBuilder.String(), fmt.Errorf("frontend build failed: %w", err)
		}
	}
	if meta.Type == "backend-only" || meta.Type == "fullstack" {
		buildCmd := "cd " + baseDir + "/backend && npm install && npm run build"
		out, err := runWorkspaceShell(ctx, wsURL, userID, buildCmd, ".")
		logBuilder.WriteString("=== backend build ===\n" + out + "\n")
		if err != nil {
			return "", logBuilder.String(), fmt.Errorf("backend build failed: %w", err)
		}
	}

	// 2. Generate + apply K8s YAML
	yamlContent := buildK8sYAML(meta.Name, meta.Type, namespace, baseDir)
	applyCmd := "echo " + shellQuote(yamlContent) + " | kubectl apply -f -"
	out, err := runWorkspaceShell(ctx, wsURL, userID, applyCmd, ".")
	logBuilder.WriteString("=== kubectl apply ===\n" + out + "\n")
	if err != nil {
		return "", logBuilder.String(), fmt.Errorf("kubectl apply failed: %w", err)
	}

	// 3. Poll pod readiness (up to 60s)
	label := "app=prototype-" + meta.Name
	pollCmd := "kubectl rollout status deployment/prototype-" + meta.Name + " -n " + namespace + " --timeout=60s"
	out, _ = runWorkspaceShell(ctx, wsURL, userID, pollCmd, ".")
	logBuilder.WriteString("=== rollout status ===\n" + out + "\n")

	deployURL := "https://" + ingressHost + "/prototypes/" + meta.Name + "/"
	_ = label
	return deployURL, logBuilder.String(), nil
}

func undeployFromK8s(ctx context.Context, userID, projectName string) (string, error) {
	wsURL := getProjectsAPIURL()
	namespace := getK8sNamespace()
	label := "app.kubernetes.io/managed-by=mcpagent-prototype,app=prototype-" + projectName
	cmd := "kubectl delete deploy,svc,ingress,configmap,secret -n " + namespace + " -l " + label + " --ignore-not-found"
	out, err := runWorkspaceShell(ctx, wsURL, userID, cmd, ".")
	return out, err
}

// ---------------------------------------------------------------------------
// Deployer: Vercel
// ---------------------------------------------------------------------------

func deployToVercel(ctx context.Context, userID string, meta PrototypeProjectMeta) (string, string, error) {
	token := os.Getenv("VERCEL_TOKEN")
	if token == "" {
		return "", "", fmt.Errorf("VERCEL_TOKEN env var not set")
	}
	wsURL := getProjectsAPIURL()
	baseDir := "/app/workspace-projects/_users/" + userID + "/Projects/" + meta.Name + "/frontend"
	buildCmd := "cd " + baseDir + " && npm install && npm run build"
	out, err := runWorkspaceShell(ctx, wsURL, userID, buildCmd, ".")
	if err != nil {
		return "", out, fmt.Errorf("vercel build failed: %w", err)
	}

	// POST to Vercel deployment API (simplified — push dist directory)
	deployCmd := "cd " + baseDir + " && npx vercel --token " + token + " --yes --name " + meta.Name + " dist/"
	deployOut, err := runWorkspaceShell(ctx, wsURL, userID, deployCmd, ".")
	out += "\n" + deployOut
	if err != nil {
		return "", out, fmt.Errorf("vercel deploy failed: %w", err)
	}

	// Extract URL from output
	deployURL := extractURLFromOutput(deployOut)
	return deployURL, out, nil
}

// ---------------------------------------------------------------------------
// Deployer: Railway
// ---------------------------------------------------------------------------

func deployToRailway(ctx context.Context, userID string, meta PrototypeProjectMeta) (string, string, error) {
	token := os.Getenv("RAILWAY_TOKEN")
	if token == "" {
		return "", "", fmt.Errorf("RAILWAY_TOKEN env var not set")
	}
	wsURL := getProjectsAPIURL()
	baseDir := "/app/workspace-projects/_users/" + userID + "/Projects/" + meta.Name + "/backend"
	cmd := "cd " + baseDir + " && RAILWAY_TOKEN=" + token + " railway up --service " + meta.Name + " --detach"
	out, err := runWorkspaceShell(ctx, wsURL, userID, cmd, ".")
	if err != nil {
		return "", out, fmt.Errorf("railway deploy failed: %w", err)
	}
	deployURL := extractURLFromOutput(out)
	return deployURL, out, nil
}

// ---------------------------------------------------------------------------
// K8s YAML template
// ---------------------------------------------------------------------------

func buildK8sYAML(name, projectType, namespace, baseDir string) string {
	label := "prototype-" + name
	managed := "mcpagent-prototype"
	var parts []string

	if projectType == "backend-only" || projectType == "fullstack" {
		parts = append(parts, fmt.Sprintf(`---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s-backend
  namespace: %s
  labels:
    app: prototype-%s
    app.kubernetes.io/managed-by: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: prototype-%s-backend
  template:
    metadata:
      labels:
        app: prototype-%s-backend
        app.kubernetes.io/managed-by: %s
    spec:
      containers:
      - name: app
        image: node:20-alpine
        command: ["node", "dist/index.js"]
        workingDir: %s/backend
        ports:
        - containerPort: 3000
        volumeMounts:
        - name: workspace
          mountPath: /app/workspace-docs
      volumes:
      - name: workspace
        persistentVolumeClaim:
          claimName: workspace-docs-pvc
---
apiVersion: v1
kind: Service
metadata:
  name: %s-backend
  namespace: %s
  labels:
    app.kubernetes.io/managed-by: %s
spec:
  selector:
    app: prototype-%s-backend
  ports:
  - port: 80
    targetPort: 3000
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: %s-backend
  namespace: %s
  labels:
    app.kubernetes.io/managed-by: %s
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /$2
spec:
  rules:
  - host: %s
    http:
      paths:
      - path: /prototypes/%s/api(/|$)(.*)
        pathType: Prefix
        backend:
          service:
            name: %s-backend
            port:
              number: 80
`, label, namespace, name, managed, name, name, managed, baseDir,
			label, namespace, managed, name,
			label, namespace, managed, getIngressHost(), name, label))
	}

	if projectType == "frontend-only" || projectType == "fullstack" {
		parts = append(parts, fmt.Sprintf(`---
apiVersion: v1
kind: ConfigMap
metadata:
  name: %s-nginx
  namespace: %s
  labels:
    app.kubernetes.io/managed-by: %s
data:
  nginx.conf: |
    server {
      listen 80;
      root %s/frontend/dist;
      index index.html;
      location /prototypes/%s/ {
        alias %s/frontend/dist/;
        try_files $uri $uri/ /index.html;
      }
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s-frontend
  namespace: %s
  labels:
    app: prototype-%s
    app.kubernetes.io/managed-by: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: prototype-%s-frontend
  template:
    metadata:
      labels:
        app: prototype-%s-frontend
        app.kubernetes.io/managed-by: %s
    spec:
      containers:
      - name: nginx
        image: nginx:alpine
        ports:
        - containerPort: 80
        volumeMounts:
        - name: workspace
          mountPath: /app/workspace-docs
        - name: nginx-conf
          mountPath: /etc/nginx/conf.d/default.conf
          subPath: nginx.conf
      volumes:
      - name: workspace
        persistentVolumeClaim:
          claimName: workspace-docs-pvc
      - name: nginx-conf
        configMap:
          name: %s-nginx
---
apiVersion: v1
kind: Service
metadata:
  name: %s-frontend
  namespace: %s
  labels:
    app.kubernetes.io/managed-by: %s
spec:
  selector:
    app: prototype-%s-frontend
  ports:
  - port: 80
    targetPort: 80
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: %s-frontend
  namespace: %s
  labels:
    app.kubernetes.io/managed-by: %s
spec:
  rules:
  - host: %s
    http:
      paths:
      - path: /prototypes/%s
        pathType: Prefix
        backend:
          service:
            name: %s-frontend
            port:
              number: 80
`, label, namespace, managed, baseDir, name, baseDir,
			label, namespace, name, managed, name, name, managed, label,
			label, namespace, managed, name,
			label, namespace, managed, getIngressHost(), name, label))
	}

	return strings.Join(parts, "\n")
}

// ---------------------------------------------------------------------------
// Env helpers
// ---------------------------------------------------------------------------

func getK8sNamespace() string {
	if ns := os.Getenv("K8S_PROTOTYPE_NAMESPACE"); ns != "" {
		return ns
	}
	return "prod-mcpagent"
}

func getIngressHost() string {
	if h := os.Getenv("INGRESS_HOST"); h != "" {
		return h
	}
	return "analytics-agent.citymall.live"
}

// ---------------------------------------------------------------------------
// Shell execution via workspace API
// ---------------------------------------------------------------------------

func runWorkspaceShell(ctx context.Context, wsURL, userID, command, workingDir string) (string, error) {
	if workingDir == "" {
		workingDir = "."
	}
	body, _ := json.Marshal(map[string]interface{}{
		"command":           command,
		"working_directory": workingDir,
		"use_shell":         true,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", wsURL+"/api/execute", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}

	client := &http.Client{Timeout: 110 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)

	var result struct {
		Data struct {
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
			ExitCode int    `json:"exit_code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		return string(b), nil
	}
	out := result.Data.Stdout
	if result.Data.Stderr != "" {
		out += "\nSTDERR: " + result.Data.Stderr
	}
	if result.Data.ExitCode != 0 {
		return out, fmt.Errorf("command exited with code %d", result.Data.ExitCode)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Misc helpers
// ---------------------------------------------------------------------------

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func extractURLFromOutput(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "https://") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// BuildPrototypeGuidance builds the system prompt for a code prototype session.
// Called by the query handler when chat_mode=="code-prototype" and prototype_project is set.
// ---------------------------------------------------------------------------

func BuildPrototypeGuidance(meta PrototypeProjectMeta) string {
	projectFolder := "Projects/" + meta.Name
	hasFrontend := meta.Type != "backend-only"
	hasBackend := meta.Type != "frontend-only"

	var lines []string
	lines = append(lines,
		fmt.Sprintf("You are a Code Prototype Assistant working on project %q.", meta.Name),
		"",
		"## Project",
		fmt.Sprintf("Path: %s/", projectFolder),
	)
	if hasFrontend {
		lines = append(lines, fmt.Sprintf("Frontend: %s/frontend/  (React 19 + Vite 7 + TypeScript)", projectFolder))
	}
	if hasBackend {
		lines = append(lines, fmt.Sprintf("Backend:  %s/backend/   (Express 5 + TypeScript)", projectFolder))
	}

	lines = append(lines,
		"",
		"## File paths",
		"All paths are relative to the workspace root. Examples:",
	)
	if hasFrontend {
		lines = append(lines,
			fmt.Sprintf("  %s/frontend/src/App.tsx", projectFolder),
			fmt.Sprintf("  %s/frontend/package.json", projectFolder),
			fmt.Sprintf("  %s/frontend/vite.config.ts", projectFolder),
		)
	}
	if hasBackend {
		lines = append(lines,
			fmt.Sprintf("  %s/backend/src/index.ts", projectFolder),
			fmt.Sprintf("  %s/backend/package.json", projectFolder),
		)
	}

	previewBase := "/api/code-prototype/preview/" + meta.Name + "/"

	lines = append(lines,
		"",
		"## Build & dev commands",
		"Each command uses its own specific working directory:",
	)

	if meta.Type == "fullstack" {
		// Fullstack: single root package.json with concurrently
		lines = append(lines,
			fmt.Sprintf(`  Install all:    execute_shell_command(command: "npm run install:all", working_directory: "%s")`, projectFolder),
			fmt.Sprintf(`  Dev (both):     execute_shell_command(command: "npm run dev &", working_directory: "%s")`, projectFolder),
			fmt.Sprintf(`    (starts frontend on port from vite.config.ts + backend; logs to %s/dev.log; preview at %s)`, projectFolder, previewBase),
			fmt.Sprintf(`  Stop:           execute_shell_command(command: "npm run stop", working_directory: "%s")`, projectFolder),
			fmt.Sprintf(`  Build frontend: execute_shell_command(command: "npm run build", working_directory: "%s/frontend")`, projectFolder),
			fmt.Sprintf(`  Build backend:  execute_shell_command(command: "npm run build", working_directory: "%s/backend")`, projectFolder),
		)
	} else {
		if hasFrontend {
			lines = append(lines,
				fmt.Sprintf(`  Install:    execute_shell_command(command: "npm install", working_directory: "%s/frontend")`, projectFolder),
				fmt.Sprintf(`  Build:      execute_shell_command(command: "npm run build", working_directory: "%s/frontend")`, projectFolder),
				fmt.Sprintf(`  Dev server: execute_shell_command(command: "VITE_BASE=%s npm run dev &", working_directory: "%s/frontend")`, previewBase, projectFolder),
				fmt.Sprintf(`    (dev server port is defined in %s/frontend/vite.config.ts; preview at %s)`, projectFolder, previewBase),
			)
		}
		if hasBackend {
			lines = append(lines,
				fmt.Sprintf(`  Install:    execute_shell_command(command: "npm install", working_directory: "%s/backend")`, projectFolder),
				fmt.Sprintf(`  Build:      execute_shell_command(command: "npm run build", working_directory: "%s/backend")`, projectFolder),
				fmt.Sprintf(`  Dev server: execute_shell_command(command: "npm run dev", working_directory: "%s/backend")`, projectFolder),
				fmt.Sprintf(`  Start:      execute_shell_command(command: "npm start", working_directory: "%s/backend")`, projectFolder),
			)
		}
	}

	if meta.Type == "fullstack" {
		lines = append(lines,
			"",
			"## Dev server logs",
			fmt.Sprintf("All dev server output (frontend + backend) is written to %s/dev.log.", projectFolder),
			"Read logs to diagnose errors:",
			fmt.Sprintf(`  Last 50 lines: execute_shell_command(command: "tail -50 dev.log", working_directory: "%s")`, projectFolder),
			fmt.Sprintf(`  Full log:      execute_shell_command(command: "cat dev.log", working_directory: "%s")`, projectFolder),
			fmt.Sprintf(`  Clear log:     execute_shell_command(command: "truncate -s 0 dev.log", working_directory: "%s")`, projectFolder),
			"When the user reports an error in the preview, ALWAYS read dev.log first before guessing the cause.",
		)
	}

	lines = append(lines,
		"",
		"## Documentation (keep up to date)",
		fmt.Sprintf("README.md:  %s/README.md", projectFolder),
		fmt.Sprintf("Docs folder: %s/docs/", projectFolder),
		"",
		"After every significant change, update the documentation:",
		fmt.Sprintf("- %s/README.md — key facts: purpose, stack, how to run, links to docs/ files", projectFolder),
		fmt.Sprintf("- %s/docs/ — detailed docs, one file per topic (e.g. docs/architecture.md, docs/api.md, docs/components.md)", projectFolder),
		fmt.Sprintf("- %s/bugs/ — known bugs and issues, one file per bug or bugs.md for a list", projectFolder),
		"README.md should be concise and reference docs/ for details.",
		"Create docs/ files as topics grow — do not dump everything into README.md.",
		"When a bug is found or fixed, update bugs/ accordingly.",
		"",
		"## Workspace boundaries",
		fmt.Sprintf("ALL files — code, plans, notes, memories — MUST be created inside %s/.", projectFolder),
		"NEVER create files at the workspace root or in Chats/, Plans/, or any other top-level folder.",
		fmt.Sprintf("Memory folder: %s/memories/", projectFolder),
		fmt.Sprintf("Plan files:    %s/plans/", projectFolder),
		"",
		"## Your role",
		"- Use workspace file tools to read and write code files",
		"- Always read an existing file before modifying it",
		"- Use execute_shell_command for npm install / build steps",
		"- When spawning sub-agents, give each a specific focused task",
		"- Keep changes minimal; summarize what changed after each task",
		"- Update README.md and relevant docs/ files after each meaningful change",
		"",
	)

	if hasFrontend {
		lines = append(lines,
			"## UI — Tailwind CSS + shadcn/ui",
			"The project is pre-configured with Tailwind v4 and shadcn/ui infrastructure:",
			fmt.Sprintf("  - Tailwind v4:   %s/frontend/src/index.css  (@import \"tailwindcss\")", projectFolder),
			fmt.Sprintf("  - cn utility:    %s/frontend/src/lib/utils.ts", projectFolder),
			fmt.Sprintf("  - shadcn config: %s/frontend/components.json", projectFolder),
			"  - Path alias @/ → src/  (e.g. import { cn } from '@/lib/utils')",
			"",
			"Add shadcn/ui components with the CLI (run from the frontend directory):",
			fmt.Sprintf(`  execute_shell_command(command: "npx shadcn@latest add button card input dialog", working_directory: "%s/frontend")`, projectFolder),
			"Components install into src/components/ui/ — import and use them directly.",
			"Full component list: https://ui.shadcn.com/docs/components",
			"Use lucide-react for icons (already installed): import { Plus, Trash2 } from 'lucide-react'",
			"",
		)
	}

	lines = append(lines,
		"## Coding guidelines",
		"- TypeScript only",
	)
	if hasFrontend {
		lines = append(lines,
			"- React: functional components + hooks",
			"- Use Tailwind utility classes for all styling — no inline styles",
			"- Use shadcn/ui components for UI; add them with: npx shadcn@latest add <component>",
			"- Use lucide-react for icons",
			"- Use cn() from @/lib/utils to merge conditional classes",
		)
	}
	if hasBackend {
		lines = append(lines, "- Express 5: async/await, errors auto-forwarded to error middleware")
	}

	lines = append(lines,
		"",
		"## Environment variables / secrets",
		fmt.Sprintf("Store all secrets and config in %s/.env (never hardcode them).", projectFolder),
		"Use dotenv to load them:",
	)
	if hasBackend {
		lines = append(lines,
			fmt.Sprintf("  Backend: add `import 'dotenv/config'` at the top of %s/backend/src/index.ts", projectFolder),
			"  Add dotenv as a dependency: npm install dotenv --prefix backend",
		)
	}
	if hasFrontend {
		lines = append(lines,
			"  Frontend: Vite automatically loads .env files — use VITE_ prefix for client-side vars (e.g. VITE_API_URL)",
			fmt.Sprintf("  Put frontend env vars in %s/frontend/.env (or %s/.env.local for secrets)", projectFolder, projectFolder),
		)
	}
	lines = append(lines,
		fmt.Sprintf("  .env is already gitignored by default. Never commit secrets."),
		"When the user mentions an API key or secret value, add it to .env and reference process.env.KEY in code.",
	)

	lines = append(lines, "", "## Git version control",
		"ALWAYS use `prototype_git` for ALL git operations — status, log, diff, add, commit, push, pull, branch, checkout, merge, everything.",
		"NEVER use `execute_shell_command` for git. The workspace container root has its own git repo — without the correct working_directory it operates on the WRONG repo. `prototype_git` always runs inside Projects/" + meta.Name + "/ and injects credentials automatically.",
		"",
		fmt.Sprintf(`  Check status:  prototype_git(project_name: %q, command: "git status")`, meta.Name),
		fmt.Sprintf(`  Save progress: prototype_git(project_name: %q, command: "git add . && git commit -m 'description'")`, meta.Name),
		fmt.Sprintf(`  View history:  prototype_git(project_name: %q, command: "git log --oneline -10")`, meta.Name),
		fmt.Sprintf(`  New branch:    prototype_git(project_name: %q, command: "git checkout -b experiment-dark-theme")`, meta.Name),
		fmt.Sprintf(`  Merge branch:  prototype_git(project_name: %q, command: "git checkout main && git merge --no-ff experiment-dark-theme")`, meta.Name),
	)
	if meta.GitHub != nil {
		lines = append(lines,
			fmt.Sprintf(`  Push to GitHub: prototype_git(project_name: %q, command: "git push origin main")`, meta.Name),
		)
	}
	lines = append(lines,
		"Save a checkpoint BEFORE making large changes.",
		"",
		"### Git best practices",
		"Follow these habits to keep the project history clean and recoverable:",
		"- Commit after every meaningful change — don't batch unrelated edits into one commit.",
		"- Use a descriptive commit message: what changed and why, not just 'update'.",
		"- Before starting a large refactor or new feature, commit the current state first.",
		"- For work spanning multiple steps, create a branch first (`git checkout -b ...`), then merge when done.",
		"- Keep branches focused — one feature or experiment per branch.",
		"- Merge branches back to main promptly once the work is verified; don't let them diverge.",
	)
	if meta.GitHub != nil {
		lines = append(lines,
			"- Push to GitHub after each commit or group of related commits so work is backed up.",
			"- Push branches too before merging, so the full history is on GitHub.",
		)
	}

	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// Preview proxy — forwards browser requests to the Vite dev server running
// inside the workspace container.
//
// Route:  GET /api/code-prototype/preview/{projectName}/{rest:.*}
// Target: http://{workspaceHost}:{WORKSPACE_DEV_PORT (default 5137)}{same path}
//
// The Vite dev server must be started with VITE_BASE matching the proxy path:
//   VITE_BASE=/api/code-prototype/preview/{name}/ npm run dev
// ---------------------------------------------------------------------------

// isSafePathSegment returns true if s contains only alphanumerics, hyphens,
// underscores, and dots — no slashes or other path-traversal characters.
func isSafePathSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// projectDevPort returns a stable port in the range 15000–15099 for a given
// project name using FNV-32a hashing. These 100 ports are mapped in docker-compose
// so the local Go server can reach dev servers running inside the workspace container.
func projectDevPort(projectName string) int {
	h := fnv.New32a()
	h.Write([]byte(projectName))
	return 15000 + int(h.Sum32()%100)
}

// containerStarting prevents hammering docker on every 5-second auto-refresh.
// key: "userID/projectName" → time.Time of last start attempt.
var containerStarting sync.Map

// ---------------------------------------------------------------------------
// Docker env helpers
// ---------------------------------------------------------------------------

// getWorkspaceDocsHostPath returns the host-machine absolute path of the workspace-docs
// directory. Required for bind-mounting project files into per-project containers because
// `docker run -v HOST_PATH:CONTAINER_PATH` always takes the host side.
// Set WORKSPACE_DOCS_HOST_PATH explicitly in Docker Compose; for local dev it defaults to
// ./workspace-docs relative to CWD.
func getWorkspaceDocsHostPath() string {
	if p := os.Getenv("WORKSPACE_DOCS_HOST_PATH"); p != "" {
		return p
	}
	if abs, err := filepath.Abs("../workspace-docs"); err == nil {
		return abs
	}
	return "../workspace-docs"
}

// getProjectsAPIURL returns the workspace-projects-api base URL.
// Code-prototype operations route here instead of workspace-api.
func getProjectsAPIURL() string {
	if u := os.Getenv("WORKSPACE_PROJECTS_API_URL"); u != "" {
		return u
	}
	return "http://localhost:9145"
}

// getProjectsHostPath returns the host-machine absolute path of the workspace-projects
// directory. Used for bind-mounting project files into per-project containers.
func getProjectsHostPath() string {
	if p := os.Getenv("WORKSPACE_PROJECTS_HOST_PATH"); p != "" {
		return p
	}
	if abs, err := filepath.Abs("../workspace-projects"); err == nil {
		return abs
	}
	return "../workspace-projects"
}

// getDockerNetwork returns the Docker network name for inter-container routing.
// If set, containers are attached to this network and reached via container name.
// If empty, host port mapping is used and proxy connects via localhost.
func getDockerNetwork() string { return os.Getenv("DOCKER_NETWORK") }

// getContainerImage returns the Node.js image to use for project containers.
func getContainerImage() string {
	if img := os.Getenv("PROTOTYPE_CONTAINER_IMAGE"); img != "" {
		return img
	}
	return "node:24-alpine"
}

// projectContainerName returns the Docker container name for a given user+project.
func projectContainerName(userID, projectName string) string {
	return "prototype-" + userID + "-" + projectName
}

// getProjectContainerURL returns the URL to proxy preview requests to, or an error
// if the container is not currently running.
func getProjectContainerURL(userID, projectName string) (string, error) {
	containerName := projectContainerName(userID, projectName)
	out, err := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", containerName).Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		return "", fmt.Errorf("container %s not running", containerName)
	}
	port := projectDevPort(projectName)
	host := "localhost"
	if net := getDockerNetwork(); net != "" {
		host = containerName
	}
	return fmt.Sprintf("http://%s:%d", host, port), nil
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

var previewWSUpgrader = websocket.Upgrader{
	CheckOrigin:  func(*http.Request) bool { return true },
	// Preserve subprotocols requested by Vite HMR client
	Subprotocols: nil,
}

func proxyWebSocketToDevServer(w http.ResponseWriter, r *http.Request, backendHost string) {
	backendURL := "ws://" + backendHost + r.URL.RequestURI()

	backendHeader := http.Header{}
	for _, sp := range websocket.Subprotocols(r) {
		backendHeader.Add("Sec-Websocket-Protocol", sp)
	}

	backendConn, _, err := websocket.DefaultDialer.Dial(backendURL, backendHeader)
	if err != nil {
		log.Printf("[PREVIEW] ws backend dial error (%s): %v", backendURL, err)
		http.Error(w, "dev server unavailable", http.StatusBadGateway)
		return
	}
	defer backendConn.Close()

	clientConn, err := previewWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[PREVIEW] ws client upgrade error: %v", err)
		return
	}
	defer clientConn.Close()

	errc := make(chan error, 2)
	bridge := func(dst, src *websocket.Conn) {
		for {
			msgType, msg, err := src.ReadMessage()
			if err != nil {
				errc <- err
				return
			}
			if err := dst.WriteMessage(msgType, msg); err != nil {
				errc <- err
				return
			}
		}
	}
	go bridge(backendConn, clientConn)
	go bridge(clientConn, backendConn)

	// Send periodic pings to both sides to keep the connection alive through
	// proxy/load-balancer idle timeouts (typically 60–75 s). Without this,
	// an idle HMR connection is silently closed, Vite detects the drop and
	// forces a full page reload instead of staying on the hot-reload path.
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()
	for {
		select {
		case err := <-errc:
			_ = err
			return
		case <-pingTicker.C:
			deadline := time.Now().Add(5 * time.Second)
			if err := clientConn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				return
			}
			if err := backendConn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				return
			}
		}
	}
}

// containerStartOnce returns true and records a start timestamp if no start was attempted
// in the last 3 minutes. Prevents hammering docker on every 5-second auto-refresh.
func containerStartOnce(startKey string) bool {
	const ttl = 3 * time.Minute
	now := time.Now()
	if v, loaded := containerStarting.Load(startKey); loaded {
		if last, ok := v.(time.Time); ok && time.Since(last) < ttl {
			return false
		}
	}
	containerStarting.Store(startKey, now)
	return true
}

// startProjectContainer creates or restarts the per-project Docker container for the
// Vite/Node dev server. Docker's -d flag makes the daemon detach immediately; npm install
// and the dev server run asynchronously inside the container.
//
// Stopped/exited containers are always removed and recreated so that volume mounts and
// config are never stale. Named node_modules volumes are preserved so npm install is
// fast on subsequent starts.
func startProjectContainer(userID, projectName string) {
	if !isSafePathSegment(userID) || !isSafePathSegment(projectName) {
		log.Printf("[PREVIEW] startProjectContainer: unsafe userID=%q or projectName=%q — aborting", userID, projectName)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Read project meta to choose volumes and start command.
	projectType := "frontend-only"
	metaContent, _ := readPrototypeFile(ctx, prototypeMetaPath(userID, projectName), userID)
	if metaContent != "" {
		var meta PrototypeProjectMeta
		if json.Unmarshal([]byte(metaContent), &meta) == nil && meta.Type != "" {
			projectType = meta.Type
		}
	}

	containerName := projectContainerName(userID, projectName)

	// Check whether the container already exists.
	statusOut, _ := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{.State.Status}}", containerName).CombinedOutput()
	status := strings.TrimSpace(string(statusOut))

	if status == "running" {
		log.Printf("[PREVIEW] container %s already running", containerName)
		return
	}
	if status == "exited" || status == "created" || status == "paused" {
		// Remove stale container so docker run recreates it with fresh mounts.
		// node_modules named volumes are NOT removed — npm cache is preserved.
		out, err := exec.CommandContext(ctx, "docker", "rm", "-f", containerName).CombinedOutput()
		log.Printf("[PREVIEW] removed stale container %s: %s (err: %v)", containerName, strings.TrimSpace(string(out)), err)
	}

	// Container doesn't exist — create and start it.
	// Use workspace-projects volume for project files (lighter, separate from workspace-docs).
	projectsHostPath := getProjectsHostPath()
	image := getContainerImage()
	port := projectDevPort(projectName)
	portStr := strconv.Itoa(port)
	projectHostPath := projectsHostPath + "/_users/" + userID + "/Projects/" + projectName
	previewBase := "/api/code-prototype/preview/" + projectName + "/"

	args := []string{
		"run", "-d",
		"--name", containerName,
		"--restart", "unless-stopped",
		"-p", portStr + ":" + portStr,
	}
	if net := getDockerNetwork(); net != "" {
		args = append(args, "--network", net)
	}

	var startCmd string
	switch projectType {
	case "fullstack":
		args = append(args,
			"-v", projectHostPath+"/frontend:/app/frontend",
			"-v", projectHostPath+"/backend:/app/backend",
			"-v", containerName+"-fe-modules:/app/frontend/node_modules",
			"-v", containerName+"-be-modules:/app/backend/node_modules",
			"-e", "VITE_BASE="+previewBase,
			"-e", "PORT=8080",
		)
		startCmd = "npm install --prefix /app/frontend && npm install --prefix /app/backend && VITE_BASE=$VITE_BASE npm run dev --prefix /app/frontend & npm run dev --prefix /app/backend & wait"
	case "frontend-only":
		args = append(args,
			"-v", projectHostPath+"/frontend:/app/frontend",
			"-v", containerName+"-fe-modules:/app/frontend/node_modules",
			"-e", "VITE_BASE="+previewBase,
		)
		startCmd = "npm install --prefix /app/frontend && VITE_BASE=$VITE_BASE npm run dev --prefix /app/frontend"
	default: // backend-only
		args = append(args,
			"-v", projectHostPath+"/backend:/app/backend",
			"-v", containerName+"-be-modules:/app/backend/node_modules",
			"-e", "PORT=8080",
		)
		startCmd = "npm install --prefix /app/backend && npm run dev --prefix /app/backend"
	}
	args = append(args, image, "sh", "-c", startCmd)

	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	log.Printf("[PREVIEW] docker run %s (type: %s): %s (err: %v)", containerName, projectType, strings.TrimSpace(string(out)), err)
}

// spinnerHTML renders the "starting…" page. Auto-refreshes every 5 seconds.
func spinnerHTML(w http.ResponseWriter, projectName, containerName string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta http-equiv="refresh" content="5"><style>
body{font-family:sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#0f172a;color:#94a3b8}
.box{text-align:center}.spinner{width:32px;height:32px;border:3px solid #334155;border-top-color:#10b981;border-radius:50%%;animation:spin 0.8s linear infinite;margin:0 auto 1rem}
@keyframes spin{to{transform:rotate(360deg)}}</style></head>
<body><div class="box"><div class="spinner"></div>
<p>Installing &amp; starting dev server for <strong style="color:#10b981">%s</strong>…</p>
<p style="font-size:0.8rem;color:#64748b">First run installs dependencies — may take a minute.</p>
<p style="font-size:0.8rem">This page refreshes automatically every 5 seconds.</p>
<p style="font-size:0.75rem;color:#475569">Check <code>docker logs %s</code> for progress.</p>
</div></body></html>`, projectName, containerName)
}

// GET /api/code-prototype/preview/{projectName}/... (PathPrefix)
func (api *StreamingAPI) handlePreviewProxy(w http.ResponseWriter, r *http.Request) {
	// Extract project name: path is /api/code-prototype/preview/{name}/...
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/code-prototype/preview/")
	projectName := strings.SplitN(trimmed, "/", 2)[0]
	if projectName == "" {
		http.NotFound(w, r)
		return
	}

	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	containerName := projectContainerName(userID, projectName)
	startKey := userID + "/" + projectName

	devURL, err := getProjectContainerURL(userID, projectName)
	if err != nil {
		// Container not running — start it and show the spinner.
		if containerStartOnce(startKey) {
			log.Printf("[PREVIEW] container not running for %s — starting", projectName)
			go func() {
				startProjectContainer(userID, projectName)
				// Clear the lock so the next proxy request retries promptly if
				// the container failed to start (instead of waiting 3 minutes).
				containerStarting.Delete(startKey)
			}()
		}
		spinnerHTML(w, projectName, containerName)
		return
	}
	target, _ := url.Parse(devURL)

	if isWebSocketUpgrade(r) {
		proxyWebSocketToDevServer(w, r, target.Host)
		return
	}

	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			// Forward the full path including the base prefix — Vite is configured
			// with VITE_BASE=/api/code-prototype/preview/{name}/ so it serves
			// index.html directly when it receives requests at that path.
			// (Stripping the base caused a redirect loop: Vite redirected "/" back
			// to its base URL, which the proxy forwarded as-is → infinite 302.)
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if containerStartOnce(startKey) {
				log.Printf("[PREVIEW] proxy error for %s: %v — re-starting container", projectName, err)
				go func() {
					startProjectContainer(userID, projectName)
					containerStarting.Delete(startKey)
				}()
			}
			spinnerHTML(w, projectName, containerName)
		},
	}
	rp.ServeHTTP(w, r)
}

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

// handleContainerLogsStream streams Docker container logs for a prototype project
// as Server-Sent Events. Each log line is emitted as an "event: log" SSE event.
// When the container stops (or is not running) an "event: done" is sent.
//
// GET /api/code-prototype/projects/{name}/logs/stream?tail=200
func (api *StreamingAPI) handleContainerLogsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	userID := GetUserIDFromContext(r.Context())
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	projectName := vars["name"]
	if projectName == "" {
		http.Error(w, "project name required", http.StatusBadRequest)
		return
	}

	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "200"
	}

	containerName := projectContainerName(userID, projectName)

	// Disable write deadline for long-lived SSE connection
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		log.Printf("[LOGS] Warning: could not disable write deadline: %v", err)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Check container status before attaching — avoids surfacing raw Docker error messages
	// when the container simply hasn't been started yet.
	statusOut, _ := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{.State.Status}}", containerName).Output()
	containerStatus := strings.TrimSpace(string(statusOut))
	if containerStatus != "running" {
		msg := "not-started"
		if containerStatus == "exited" || containerStatus == "created" || containerStatus == "paused" {
			msg = "stopped"
		}
		fmt.Fprintf(w, "event: done\ndata: {\"message\":%q}\n\n", msg)
		flusher.Flush()
		return
	}

	cmd := exec.CommandContext(ctx, "docker", "logs", "-f", "--tail="+tail, "--timestamps", containerName)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeContainerLogEvent(w, "error", "", "failed to create pipe: "+err.Error())
		flusher.Flush()
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		writeContainerLogEvent(w, "error", "", "failed to create pipe: "+err.Error())
		flusher.Flush()
		return
	}
	if err := cmd.Start(); err != nil {
		writeContainerLogEvent(w, "error", "", "container not found or not running: "+containerName)
		flusher.Flush()
		return
	}

	log.Printf("[LOGS] streaming %s (user=%s tail=%s)", containerName, userID, tail)

	lines := make(chan string, 128)
	var wg sync.WaitGroup

	scanPipe := func(rd io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(rd)
		sc.Buffer(make([]byte, 64*1024), 64*1024)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}
	wg.Add(2)
	go scanPipe(stdout)
	go scanPipe(stderr)
	go func() { wg.Wait(); close(lines) }()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-lines:
			if !ok {
				log.Printf("[LOGS] container %s stopped, closing stream", containerName)
				fmt.Fprintf(w, "event: done\ndata: {\"message\":\"container stopped\"}\n\n")
				flusher.Flush()
				return
			}
			writeContainerLogEvent(w, "log", line, "")
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func writeContainerLogEvent(w http.ResponseWriter, eventType, line, errMsg string) {
	type payload struct {
		Line  string `json:"line,omitempty"`
		Error string `json:"error,omitempty"`
	}
	p := payload{}
	if eventType == "error" {
		p.Error = errMsg
	} else {
		p.Line = line
	}
	b, _ := json.Marshal(p)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, b)
}

func RegisterCodePrototypeRoutes(apiRouter *mux.Router, api *StreamingAPI) {
	apiRouter.HandleFunc("/code-prototype/projects", api.handleListPrototypeProjects).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects", api.handleCreatePrototypeProject).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}", api.handleGetPrototypeProject).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}/logs/stream", api.handleContainerLogsStream).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}/config", api.handleUpdatePrototypeConfig).Methods("PATCH", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}", api.handleDeletePrototypeProject).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/deploy", api.handleDeployPrototype).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/deploy/{projectName}", api.handleUndeployPrototype).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/stop-dev", api.handleStopDevServers).Methods("POST", "OPTIONS")

	// GitHub version control routes
	apiRouter.HandleFunc("/code-prototype/projects/{name}/github/connect", api.handleGitHubConnect).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}/github", api.handleGitHubDisconnect).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}/github/status", api.handleGitHubStatus).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}/github/checkpoint", api.handleGitHubSaveCheckpoint).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}/github/history", api.handleGitHubHistory).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}/github/restore", api.handleGitHubRestore).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}/github/publish", api.handleGitHubPublish).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}/github/pull", api.handleGitHubPull).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}/github/experiments", api.handleGitHubListExperiments).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}/github/experiments", api.handleGitHubStartExperiment).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}/github/experiments/keep", api.handleGitHubKeepExperiment).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}/github/experiments/current", api.handleGitHubDiscardExperiment).Methods("DELETE", "OPTIONS")

	// Preview proxy — must be registered last (PathPrefix catch-all)
	apiRouter.PathPrefix("/code-prototype/preview/").HandlerFunc(api.handlePreviewProxy)
}
