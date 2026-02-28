package server

import (
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
	"regexp"
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

// Frontend scaffold — React 19 + Vite 7 + TypeScript 5.9 (2026 best practices)
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
    "react": "^19.2.0",
    "react-dom": "^19.2.0"
  },
  "devDependencies": {
    "@types/react": "^19.0.0",
    "@types/react-dom": "^19.0.0",
    "@vitejs/plugin-react-swc": "^3.10.0",
    "typescript": "~5.9.0",
    "vite": "^7.0.0"
  }
}
`

const scaffoldViteConfig = `import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react-swc'

export default defineConfig({
  plugins: [react()],
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
    "noUncheckedSideEffectImports": true
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
    <div style={{ fontFamily: 'sans-serif', padding: '2rem' }}>
      <h1>{{PROJECT_NAME}}</h1>
      <p>Count: {count}</p>
      <button onClick={() => setCount(c => c + 1)}>Increment</button>
    </div>
  )
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
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath

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

	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath + "/raw"
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

	apiURL := getWorkspaceAPIURL() + "/api/folders/" + encodedPath + "?confirm=true"
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

	replacer := strings.NewReplacer(
		"{{PROJECT_NAME}}", name,
		"{{DEV_PORT}}", strconv.Itoa(projectDevPort(name)),
	)

	files := map[string]string{}

	if meta.Type == "frontend-only" || meta.Type == "fullstack" {
		files[base+"/frontend/package.json"] = replacer.Replace(scaffoldFrontendPackageJSON)
		files[base+"/frontend/vite.config.ts"] = replacer.Replace(scaffoldViteConfig)
		files[base+"/frontend/tsconfig.json"] = replacer.Replace(scaffoldFrontendTsConfig)
		files[base+"/frontend/tsconfig.app.json"] = replacer.Replace(scaffoldFrontendTsConfigApp)
		files[base+"/frontend/tsconfig.node.json"] = replacer.Replace(scaffoldFrontendTsConfigNode)
		files[base+"/frontend/index.html"] = replacer.Replace(scaffoldIndexHTML)
		files[base+"/frontend/src/main.tsx"] = replacer.Replace(scaffoldMainTsx)
		files[base+"/frontend/src/App.tsx"] = replacer.Replace(scaffoldAppTsx)
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

	// List subfolders of Projects/ via workspace API (X-User-ID scopes to this user)
	wsURL := getWorkspaceAPIURL() + "/api/documents?folder=Projects"
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
	wsURL := getWorkspaceAPIURL()
	baseDir := "/app/workspace-docs/_users/" + userID + "/Projects/" + meta.Name
	namespace := getK8sNamespace()
	ingressHost := getIngressHost()

	// 1. Build step
	if meta.Type == "frontend-only" || meta.Type == "fullstack" {
		buildCmd := "cd " + baseDir + "/frontend && npm install && npm run build"
		out, err := runWorkspaceShell(ctx, wsURL, userID, buildCmd)
		logBuilder.WriteString("=== frontend build ===\n" + out + "\n")
		if err != nil {
			return "", logBuilder.String(), fmt.Errorf("frontend build failed: %w", err)
		}
	}
	if meta.Type == "backend-only" || meta.Type == "fullstack" {
		buildCmd := "cd " + baseDir + "/backend && npm install && npm run build"
		out, err := runWorkspaceShell(ctx, wsURL, userID, buildCmd)
		logBuilder.WriteString("=== backend build ===\n" + out + "\n")
		if err != nil {
			return "", logBuilder.String(), fmt.Errorf("backend build failed: %w", err)
		}
	}

	// 2. Generate + apply K8s YAML
	yamlContent := buildK8sYAML(meta.Name, meta.Type, namespace, baseDir)
	applyCmd := "echo " + shellQuote(yamlContent) + " | kubectl apply -f -"
	out, err := runWorkspaceShell(ctx, wsURL, userID, applyCmd)
	logBuilder.WriteString("=== kubectl apply ===\n" + out + "\n")
	if err != nil {
		return "", logBuilder.String(), fmt.Errorf("kubectl apply failed: %w", err)
	}

	// 3. Poll pod readiness (up to 60s)
	label := "app=prototype-" + meta.Name
	pollCmd := "kubectl rollout status deployment/prototype-" + meta.Name + " -n " + namespace + " --timeout=60s"
	out, _ = runWorkspaceShell(ctx, wsURL, userID, pollCmd)
	logBuilder.WriteString("=== rollout status ===\n" + out + "\n")

	deployURL := "https://" + ingressHost + "/prototypes/" + meta.Name + "/"
	_ = label
	return deployURL, logBuilder.String(), nil
}

func undeployFromK8s(ctx context.Context, userID, projectName string) (string, error) {
	wsURL := getWorkspaceAPIURL()
	namespace := getK8sNamespace()
	label := "app.kubernetes.io/managed-by=mcpagent-prototype,app=prototype-" + projectName
	cmd := "kubectl delete deploy,svc,ingress,configmap,secret -n " + namespace + " -l " + label + " --ignore-not-found"
	out, err := runWorkspaceShell(ctx, wsURL, userID, cmd)
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
	wsURL := getWorkspaceAPIURL()
	baseDir := "/app/workspace-docs/_users/" + userID + "/Projects/" + meta.Name + "/frontend"
	buildCmd := "cd " + baseDir + " && npm install && npm run build"
	out, err := runWorkspaceShell(ctx, wsURL, userID, buildCmd)
	if err != nil {
		return "", out, fmt.Errorf("vercel build failed: %w", err)
	}

	// POST to Vercel deployment API (simplified — push dist directory)
	deployCmd := "cd " + baseDir + " && npx vercel --token " + token + " --yes --name " + meta.Name + " dist/"
	deployOut, err := runWorkspaceShell(ctx, wsURL, userID, deployCmd)
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
	wsURL := getWorkspaceAPIURL()
	baseDir := "/app/workspace-docs/_users/" + userID + "/Projects/" + meta.Name + "/backend"
	cmd := "cd " + baseDir + " && RAILWAY_TOKEN=" + token + " railway up --service " + meta.Name + " --detach"
	out, err := runWorkspaceShell(ctx, wsURL, userID, cmd)
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

func runWorkspaceShell(ctx context.Context, wsURL, userID, command string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"command":           command,
		"working_directory": ".",
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

	lines = append(lines,
		"",
		"## Build & dev commands",
		`All commands run from the workspace root (working_directory: "."):`,
	)
	if hasFrontend {
		previewBase := "/api/code-prototype/preview/" + meta.Name + "/"
		lines = append(lines,
			fmt.Sprintf(`  Install:    execute_shell_command(command: "cd %s/frontend && npm install", working_directory: ".")`, projectFolder),
			fmt.Sprintf(`  Build:      execute_shell_command(command: "cd %s/frontend && npm run build", working_directory: ".")`, projectFolder),
			fmt.Sprintf(`  Dev server: execute_shell_command(command: "cd %s/frontend && VITE_BASE=%s npm run dev &", working_directory: ".")`, projectFolder, previewBase),
			fmt.Sprintf(`    (dev server port is defined in %s/frontend/vite.config.ts; preview at %s)`, projectFolder, previewBase),
		)
	}
	if hasBackend {
		lines = append(lines,
			fmt.Sprintf(`  Install:    execute_shell_command(command: "cd %s/backend && npm install", working_directory: ".")`, projectFolder),
			fmt.Sprintf(`  Build:      execute_shell_command(command: "cd %s/backend && npm run build", working_directory: ".")`, projectFolder),
			fmt.Sprintf(`  Dev server: execute_shell_command(command: "cd %s/backend && npm run dev", working_directory: ".")`, projectFolder),
			fmt.Sprintf(`  Start:      execute_shell_command(command: "cd %s/backend && npm start", working_directory: ".")`, projectFolder),
		)
	}

	lines = append(lines,
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
		"",
		"## Coding guidelines",
		"- TypeScript only",
	)
	if hasFrontend {
		lines = append(lines, "- React: functional components + hooks")
	}
	if hasBackend {
		lines = append(lines, "- Express 5: async/await, errors auto-forwarded to error middleware")
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

// projectDevPort returns a stable port in the range 15000–15019 for a given
// project name using FNV-32a hashing. These 20 ports are mapped in docker-compose
// so the local Go server can reach dev servers running inside the workspace container.
func projectDevPort(projectName string) int {
	h := fnv.New32a()
	h.Write([]byte(projectName))
	return 15000 + int(h.Sum32()%20)
}

var (
	vitePortRe   = regexp.MustCompile(`port:\s*(\d+)`)
	devPortCache sync.Map // key "userID/project" → devPortCacheEntry
)

type devPortCacheEntry struct {
	port      int
	expiresAt time.Time
}

// devPortFromViteConfig reads Projects/{name}/frontend/vite.config.ts and
// extracts the `port: <N>` value. Falls back to the hash-based default if
// the file is missing or the port line is absent.
func devPortFromViteConfig(ctx context.Context, userID, projectName string) int {
	configPath := prototypeFolderPath(userID, projectName) + "/frontend/vite.config.ts"
	content, _ := readPrototypeFile(ctx, configPath, userID)
	if m := vitePortRe.FindStringSubmatch(content); len(m) == 2 {
		if p, err := strconv.Atoi(m[1]); err == nil && p > 0 {
			return p
		}
	}
	return projectDevPort(projectName) // fallback
}

func getWorkspaceDevServerURL(ctx context.Context, userID, projectName string) (string, error) {
	cacheKey := userID + "/" + projectName
	if v, ok := devPortCache.Load(cacheKey); ok {
		entry := v.(devPortCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			wsAPIURL := getWorkspaceAPIURL()
			u, _ := url.Parse(wsAPIURL)
			u.Host = u.Hostname() + ":" + strconv.Itoa(entry.port)
			return u.String(), nil
		}
	}

	port := devPortFromViteConfig(ctx, userID, projectName)
	devPortCache.Store(cacheKey, devPortCacheEntry{port: port, expiresAt: time.Now().Add(60 * time.Second)})

	wsAPIURL := getWorkspaceAPIURL()
	u, err := url.Parse(wsAPIURL)
	if err != nil {
		return "", fmt.Errorf("invalid workspace API URL: %w", err)
	}
	u.Host = u.Hostname() + ":" + strconv.Itoa(port)
	return u.String(), nil
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
	<-errc
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	devURL, err := getWorkspaceDevServerURL(ctx, userID, projectName)
	if err != nil {
		http.Error(w, "preview unavailable: "+err.Error(), http.StatusServiceUnavailable)
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
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			log.Printf("[PREVIEW] proxy error: %v", err)
			http.Error(w, "dev server unavailable — run 'npm run dev' in the project's frontend folder", http.StatusBadGateway)
		},
	}
	rp.ServeHTTP(w, r)
}

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

func RegisterCodePrototypeRoutes(apiRouter *mux.Router, api *StreamingAPI) {
	apiRouter.HandleFunc("/code-prototype/projects", api.handleListPrototypeProjects).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects", api.handleCreatePrototypeProject).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}", api.handleGetPrototypeProject).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}/config", api.handleUpdatePrototypeConfig).Methods("PATCH", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/projects/{name}", api.handleDeletePrototypeProject).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/deploy", api.handleDeployPrototype).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/code-prototype/deploy/{projectName}", api.handleUndeployPrototype).Methods("DELETE", "OPTIONS")
	// Preview proxy — must be registered last (PathPrefix catch-all)
	apiRouter.PathPrefix("/code-prototype/preview/").HandlerFunc(api.handlePreviewProxy)
}
