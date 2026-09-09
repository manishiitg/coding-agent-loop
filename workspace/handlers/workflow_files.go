package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	wf "github.com/manishiitg/coding-agent-loop/workspace/workflowfiles"
	"github.com/spf13/viper"
)

// Coordinate API document writers with revision-checked external transactions.
// Shell writers are outside this lock; planning files are already shell-guarded.
var workflowFilesMu sync.Mutex

func WorkflowDocumentLock(c *gin.Context) {
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
		c.Next()
		return
	}
	workflowFilesMu.Lock()
	defer workflowFilesMu.Unlock()
	c.Next()
}

type workflowFileError struct {
	status  int
	message string
}

func (e *workflowFileError) Error() string { return e.message }
func fileError(status int, format string, args ...any) error {
	return &workflowFileError{status, fmt.Sprintf(format, args...)}
}

// WorkflowFiles is an internal workspace-service endpoint. The agent server
// resolves and authorizes Root; callers never supply an absolute host path.
func WorkflowFiles(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<20)
	var req wf.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	workflowFilesMu.Lock()
	defer workflowFilesMu.Unlock()
	result, err := executeWorkflowFiles(c, req)
	if err != nil {
		status := 500
		var fe *workflowFileError
		if errors.As(err, &fe) {
			status = fe.status
		} else if errors.Is(err, fs.ErrNotExist) {
			status = 404
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, result)
}
func noSymlinks(root *os.Root, p string) error {
	prefix := ""
	for _, part := range strings.Split(p, "/") {
		if part == "." || part == "" {
			continue
		}
		prefix = path.Join(prefix, part)
		st, err := root.Lstat(prefix)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fileError(403, "symbolic links are not exposed by workflow file tools")
		}
	}
	return nil
}
func scopedFile(root *os.Root, p string) (wf.File, error) {
	result := wf.File{Path: p, Revision: wf.MissingRevision}
	if err := noSymlinks(root, p); err != nil {
		return result, err
	}
	entry, err := root.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if !entry.Mode().IsRegular() {
		return result, fileError(400, "path is not a regular file")
	}
	f, err := root.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return result, err
	}
	if !st.Mode().IsRegular() {
		return result, fileError(400, "path is not a regular file")
	}
	if st.Size() > wf.MaxFileBytes {
		return result, fileError(413, "file exceeds %d byte limit", wf.MaxFileBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, wf.MaxFileBytes+1))
	if err != nil {
		return result, err
	}
	if len(data) > wf.MaxFileBytes {
		return result, fileError(413, "file exceeds limit")
	}
	result.Exists = true
	result.Size = int64(len(data))
	result.Revision = wf.Revision(data)
	result.Encoding = "utf-8"
	if utf8.Valid(data) && !strings.ContainsRune(string(data), 0) {
		result.Content = string(data)
	} else {
		result.Encoding = "base64"
		result.Content = base64.StdEncoding.EncodeToString(data)
	}
	return result, nil
}
func executeWorkflowFiles(c *gin.Context, req wf.Request) (wf.Result, error) {
	rootPath, err := wf.CleanRelative(req.Root)
	if err != nil {
		return wf.Result{}, fileError(400, "invalid workflow root")
	}
	parts := strings.Split(rootPath, "/")
	if len(parts) != 2 || parts[0] != "Workflow" || parts[1] == "." {
		return wf.Result{}, fileError(400, "root must identify one workflow")
	}
	base, err := os.OpenRoot(viper.GetString("docs-dir"))
	if err != nil {
		return wf.Result{}, err
	}
	defer base.Close()
	if err = noSymlinks(base, rootPath); err != nil {
		return wf.Result{}, err
	}
	root, err := base.OpenRoot(rootPath)
	if err != nil {
		return wf.Result{}, err
	}
	defer root.Close()
	p, err := wf.CleanRelative(req.Path)
	if err != nil {
		return wf.Result{}, fileError(400, "%s", err)
	}
	if wf.Private(p) && !req.Managed {
		return wf.Result{}, fileError(403, "private workflow path; use the builder chat API for conversations")
	}
	if err = noSymlinks(root, p); err != nil {
		return wf.Result{}, err
	}
	switch req.Operation {
	case "read":
		f, e := scopedFile(root, p)
		return wf.Result{File: f}, e
	case "list", "search":
		return listScopedFiles(c, root, p, req)
	case "write", "patch":
		if wf.Protected(p) {
			return wf.Result{}, fileError(403, "protected workflow artifact; use a typed plan tool")
		}
		current, e := scopedFile(root, p)
		if e != nil {
			return wf.Result{}, e
		}
		if req.ExpectedRevision == "" || req.ExpectedRevision != current.Revision {
			return wf.Result{}, fileError(409, "revision conflict: read the file and retry with its current revision")
		}
		content := req.Content
		if req.Operation == "patch" {
			if current.Encoding == "base64" {
				return wf.Result{}, fileError(400, "cannot patch a binary file")
			}
			content, e = applyDiffPatch(c.Request.Context(), current.Content, req.Diff)
			if e != nil {
				return wf.Result{}, fileError(400, "invalid patch: %s", e)
			}
		}
		if e = commitScopedFiles(root, map[string]string{p: current.Revision}, map[string]string{p: content}); e != nil {
			return wf.Result{}, e
		}
		f, e := scopedFile(root, p)
		f.Content = ""
		return wf.Result{File: f}, e
	case "commit":
		if !req.Managed {
			return wf.Result{}, fileError(403, "commit is only available to server-managed plan operations")
		}
		if err = commitScopedFiles(root, req.Checks, req.Writes); err != nil {
			return wf.Result{}, err
		}
		revisions := map[string]string{}
		for p, content := range req.Writes {
			revisions[p] = wf.Revision([]byte(content))
		}
		return wf.Result{Revisions: revisions}, nil
	default:
		return wf.Result{}, fileError(400, "unknown workflow file operation")
	}
}
func listScopedFiles(c *gin.Context, root *os.Root, p string, req wf.Request) (wf.Result, error) {
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Limit > 200 || req.Offset < 0 || req.Offset > 10000 {
		return wf.Result{}, fileError(400, "invalid pagination")
	}
	if req.Depth <= 0 {
		req.Depth = 4
	}
	if req.Depth > 8 {
		return wf.Result{}, fileError(400, "depth exceeds 8")
	}
	if req.Operation == "search" && req.Query == "" {
		return wf.Result{}, fileError(400, "query is required")
	}
	all := make([]wf.Entry, 0)
	visited := 0
	var scannedBytes int64
	truncated := false
	err := fs.WalkDir(root.FS(), p, func(name string, d fs.DirEntry, walkErr error) error {
		if err := c.Request.Context().Err(); err != nil {
			return err
		}
		visited++
		if visited > 10000 {
			truncated = true
			return fs.SkipAll
		}
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if wf.Private(name) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(name, p), "/")
		if name == p && d.IsDir() {
			return nil
		}
		if strings.Count(rel, "/") >= req.Depth {
			if d.IsDir() {
				truncated = true
				return fs.SkipDir
			}
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		entry := wf.Entry{Path: name, Type: "file", Size: st.Size()}
		if d.IsDir() {
			entry.Type = "folder"
		}
		if req.Operation == "list" {
			all = append(all, entry)
		} else if st.Mode().IsRegular() && st.Size() <= wf.MaxFileBytes {
			scannedBytes += st.Size()
			if scannedBytes > 64<<20 {
				truncated = true
				return fs.SkipAll
			}
			f, e := scopedFile(root, name)
			if e != nil {
				return e
			}
			if f.Encoding == "utf-8" {
				for n, line := range strings.Split(f.Content, "\n") {
					if strings.Contains(strings.ToLower(line), strings.ToLower(req.Query)) {
						match := entry
						match.Line = n + 1
						if len(line) > 1000 {
							line = line[:1000]
						}
						match.Text = line
						all = append(all, match)
						if len(all) > req.Offset+req.Limit {
							break
						}
					}
				}
			}
		}
		if len(all) > req.Offset+req.Limit {
			truncated = true
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return wf.Result{}, err
	}
	start := min(req.Offset, len(all))
	end := min(start+req.Limit, len(all))
	result := wf.Result{Entries: all[start:end], Truncated: truncated}
	if len(all) > end {
		result.NextOffset = end
	}
	return result, nil
}

// Stage every replacement before changing any file. Scoped reads and API
// document writers share workflowFilesMu. Roll back ordinary IO failures; this is not crash recovery.
func commitScopedFiles(root *os.Root, checks, writes map[string]string) error {
	if len(writes) > 100 || len(checks) > 300 {
		return fileError(413, "transaction exceeds file limit")
	}
	keys := make([]string, 0, len(writes))
	old := map[string]wf.File{}
	total := 0
	for p, want := range checks {
		cleaned, e := wf.CleanRelative(p)
		if e != nil || cleaned != p || wf.Private(p) {
			return fileError(403, "invalid transaction path")
		}
		f, e := scopedFile(root, p)
		if e != nil {
			return e
		}
		if want != f.Revision {
			return fileError(409, "revision conflict for %s; read current state before retrying", p)
		}
	}
	for p, content := range writes {
		cleaned, e := wf.CleanRelative(p)
		if e != nil || cleaned != p || wf.Private(p) || p == "workflow.json" {
			return fileError(403, "invalid transaction path")
		}
		if _, ok := checks[p]; !ok {
			return fileError(400, "write missing revision check")
		}
		total += len(content)
		if len(content) > wf.MaxFileBytes || total > 12<<20 {
			return fileError(413, "transaction exceeds content limit")
		}
		f, e := scopedFile(root, p)
		if e != nil {
			return e
		}
		old[p] = f
		keys = append(keys, p)
	}
	sort.Strings(keys)
	temps := map[string]string{}
	defer func() {
		for _, tmp := range temps {
			_ = root.Remove(tmp)
		}
	}()
	for _, p := range keys {
		if e := root.MkdirAll(path.Dir(p), 0755); e != nil {
			return e
		}
		tmp := path.Join(path.Dir(p), ".agentworks-"+uuid.NewString()+".tmp")
		f, e := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		temps[p] = tmp
		if previous, statErr := root.Stat(p); statErr == nil {
			if e = f.Chmod(previous.Mode().Perm()); e != nil {
				f.Close()
				return e
			}
		}
		_, e = f.WriteString(writes[p])
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	done := []string{}
	for _, p := range keys {
		if err := root.Rename(temps[p], p); err != nil {
			var rollbackErr error
			for i := len(done) - 1; i >= 0; i-- {
				name := done[i]
				before := old[name]
				if !before.Exists {
					rollbackErr = errors.Join(rollbackErr, root.Remove(name))
					continue
				}
				data := []byte(before.Content)
				if before.Encoding == "base64" {
					data, _ = base64.StdEncoding.DecodeString(before.Content)
				}
				rollbackErr = errors.Join(rollbackErr, root.WriteFile(name, data, 0600))
			}
			if rollbackErr != nil {
				return fileError(500, "commit failed and rollback incomplete: %v; %v", err, rollbackErr)
			}
			return err
		}
		delete(temps, p)
		done = append(done, p)
	}
	return nil
}
