package models

// QueryRequest represents a read-only SQL query against a workflow's SQLite DB.
type QueryRequest struct {
	// DBPath is a workspace-relative path to the SQLite file,
	// e.g. "Workflow/social-media/db/db.sqlite".
	DBPath string `json:"db_path" binding:"required"`
	// SQL is the read-only query to run. The connection is opened read-only
	// (mode=ro + query_only) so writes are rejected by the engine; the caller
	// (the report widget author) owns LIMIT/shape of the result.
	SQL string `json:"sql" binding:"required"`
}

// QueryResponse is the result of a read-only SQL query: rows are returned as an
// array of objects keyed by column name so report widget renderers consume them
// the same way they consumed parsed JSON arrays.
type QueryResponse struct {
	Columns []string                 `json:"columns"`
	Rows    []map[string]interface{} `json:"rows"`
}

// DBTablesResponse describes the tables in a workflow's SQLite DB, for the
// read-only DatabasePopup inspector.
type DBTablesResponse struct {
	Tables []DBTableInfo `json:"tables"`
}

type DBTableInfo struct {
	Name     string                   `json:"name"`
	Columns  []DBColumnInfo           `json:"columns"`
	RowCount int64                    `json:"row_count"`
	Sample   []map[string]interface{} `json:"sample"`
}

type DBColumnInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	PrimaryKey bool   `json:"primary_key"`
}
