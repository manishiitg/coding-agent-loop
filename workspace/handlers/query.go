package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/manishiitg/mcp-agent-builder-go/workspace/models"
	"github.com/manishiitg/mcp-agent-builder-go/workspace/utils"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"

	_ "modernc.org/sqlite"
)

// queryTimeout bounds a single read-only query / inspection request.
const queryTimeout = 30 * time.Second

// dbTablesSampleRows is how many sample rows the inspector returns per table.
const dbTablesSampleRows = 50

// resolveReadonlyDBPath resolves a workspace-relative SQLite path with the same
// user-isolation rules as document access, and rejects cross-user (_users/)
// access and traversal. Returns the absolute filesystem path.
func resolveReadonlyDBPath(c *gin.Context, requestedPath string) (string, error) {
	docsDir := viper.GetString("docs-dir")
	clean := utils.SanitizeInputPath(requestedPath, docsDir)
	// Never allow a query endpoint to reach into another user's private tree.
	if clean == utils.UsersDirectory || strings.HasPrefix(clean, utils.UsersDirectory+"/") {
		return "", fmt.Errorf("access to %s/ is not allowed", utils.UsersDirectory)
	}
	fullPath, err := resolveUserPath(c, requestedPath)
	if err != nil {
		return "", err
	}
	if !utils.IsValidFilePath(fullPath, docsDir) {
		return "", fmt.Errorf("path escapes the workspace boundary")
	}
	if info, err := os.Stat(fullPath); err != nil || info.IsDir() {
		return "", fmt.Errorf("database file not found: %s", requestedPath)
	}
	return fullPath, nil
}

// openReadonlyDB opens a SQLite file read-only. mode=ro fails any write at the
// driver level; query_only is a pragma backstop; busy_timeout avoids spurious
// SQLITE_BUSY while a workflow step holds a write lock.
func openReadonlyDB(fullPath string) (*sql.DB, error) {
	dsn := "file:" + fullPath + "?mode=ro&_pragma=query_only(true)&_pragma=busy_timeout(5000)"
	return sql.Open("sqlite", dsn)
}

// scanRows reads all rows of a *sql.Rows into []map[string]interface{}, keyed by
// column name. []byte values are coerced to string so JSON output is readable.
func scanRows(rows *sql.Rows) ([]string, []map[string]interface{}, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	out := make([]map[string]interface{}, 0)
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		row := make(map[string]interface{}, len(cols))
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				row[c] = string(b)
			} else {
				row[c] = vals[i]
			}
		}
		out = append(out, row)
	}
	return cols, out, rows.Err()
}

// QueryWorkflowDB handles POST /api/query — runs a read-only SQL query against a
// workflow's db/db.sqlite and returns rows as an array of objects.
func QueryWorkflowDB(c *gin.Context) {
	var req models.QueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false, Message: "Invalid request body", Error: err.Error(),
		})
		return
	}
	if strings.TrimSpace(req.SQL) == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false, Message: "sql is required", Error: "sql cannot be empty",
		})
		return
	}

	fullPath, err := resolveReadonlyDBPath(c, req.DBPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false, Message: "Invalid db_path", Error: err.Error(),
		})
		return
	}

	db, err := openReadonlyDB(fullPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false, Message: "Failed to open database", Error: err.Error(),
		})
		return
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(c.Request.Context(), queryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, req.SQL)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false, Message: "Query failed", Error: err.Error(),
		})
		return
	}
	defer rows.Close()

	cols, data, err := scanRows(rows)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false, Message: "Failed to read rows", Error: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse[models.QueryResponse]{
		Success: true,
		Data:    models.QueryResponse{Columns: cols, Rows: data},
	})
}

// GetWorkflowDBTables handles GET /api/db/tables?db_path=... — lists tables,
// per-table schema, row count and a small sample, for the DatabasePopup viewer.
func GetWorkflowDBTables(c *gin.Context) {
	dbPath := c.Query("db_path")
	if strings.TrimSpace(dbPath) == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false, Message: "db_path is required", Error: "db_path query parameter cannot be empty",
		})
		return
	}

	fullPath, err := resolveReadonlyDBPath(c, dbPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false, Message: "Invalid db_path", Error: err.Error(),
		})
		return
	}

	db, err := openReadonlyDB(fullPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false, Message: "Failed to open database", Error: err.Error(),
		})
		return
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(c.Request.Context(), queryTimeout)
	defer cancel()

	tableNames, err := listTableNames(ctx, db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false, Message: "Failed to list tables", Error: err.Error(),
		})
		return
	}

	tables := make([]models.DBTableInfo, 0, len(tableNames))
	for _, name := range tableNames {
		info := models.DBTableInfo{Name: name}

		if cols, err := tableColumns(ctx, db, name); err == nil {
			info.Columns = cols
		}

		quoted := quoteIdent(name)
		_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoted).Scan(&info.RowCount)

		if sampleRows, err := db.QueryContext(ctx, "SELECT * FROM "+quoted+" LIMIT ?", dbTablesSampleRows); err == nil {
			if _, data, err := scanRows(sampleRows); err == nil {
				info.Sample = data
			}
			sampleRows.Close()
		}

		tables = append(tables, info)
	}

	c.JSON(http.StatusOK, models.APIResponse[models.DBTablesResponse]{
		Success: true,
		Data:    models.DBTablesResponse{Tables: tables},
	})
}

// listTableNames returns user table names (excluding sqlite_* internal tables).
func listTableNames(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// tableColumns returns column metadata via PRAGMA table_info.
func tableColumns(ctx context.Context, db *sql.DB, table string) ([]models.DBColumnInfo, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+quoteIdent(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []models.DBColumnInfo
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notNull   int
			dfltValue interface{}
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, models.DBColumnInfo{Name: name, Type: ctype, PrimaryKey: pk > 0})
	}
	return cols, rows.Err()
}

// quoteIdent quotes a SQLite identifier (table/column) by doubling embedded
// double-quotes. Names come from sqlite_master (already-created objects), so
// this is defense-in-depth, not untrusted input.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
