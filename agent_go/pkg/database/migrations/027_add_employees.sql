CREATE TABLE IF NOT EXISTS employees (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    name TEXT NOT NULL,
    avatar_color TEXT DEFAULT '#6366f1',
    description TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    user_id TEXT DEFAULT NULL
);
CREATE INDEX IF NOT EXISTS idx_employees_user_id ON employees(user_id);

ALTER TABLE preset_queries ADD COLUMN employee_id TEXT DEFAULT NULL REFERENCES employees(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_preset_queries_employee_id ON preset_queries(employee_id);
