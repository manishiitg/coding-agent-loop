package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Migration represents a database migration
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// MigrationRunner handles database migrations
type MigrationRunner struct {
	db *sql.DB
}

// NewMigrationRunner creates a new migration runner
func NewMigrationRunner(db *sql.DB) *MigrationRunner {
	return &MigrationRunner{db: db}
}

// RunMigrations runs all pending migrations
func (mr *MigrationRunner) RunMigrations(migrationsDir string) error {
	// Create migrations table if it doesn't exist
	if err := mr.createMigrationsTable(); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Load migration files
	migrations, err := mr.loadMigrations(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Get applied migrations
	appliedMigrations, err := mr.getAppliedMigrations()
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	fmt.Printf("📊 Found %d migration files, %d already applied\n", len(migrations), len(appliedMigrations))

	// Run pending migrations
	for _, migration := range migrations {
		if !mr.isMigrationApplied(migration.Version, appliedMigrations) {
			fmt.Printf("🔄 Running migration %d: %s\n", migration.Version, migration.Name)
			if err := mr.runMigration(migration); err != nil {
				return fmt.Errorf("failed to run migration %d (%s): %w", migration.Version, migration.Name, err)
			}
		} else {
			fmt.Printf("⏭️  Skipping migration %d: %s (already applied)\n", migration.Version, migration.Name)
		}
	}

	return nil
}

// createMigrationsTable creates the migrations tracking table
func (mr *MigrationRunner) createMigrationsTable() error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`
	_, err := mr.db.Exec(query)
	return err
}

// loadMigrations loads migration files from the migrations directory
func (mr *MigrationRunner) loadMigrations(migrationsDir string) ([]Migration, error) {
	var migrations []Migration

	// Read migration files from the filesystem
	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	if err != nil {
		return nil, fmt.Errorf("failed to read migration directory: %w", err)
	}

	// Debug output removed for cleaner logs

	for _, file := range files {
		// Extract version number from filename (e.g., "001_add_workflow_status.sql" -> 1)
		filename := filepath.Base(file)
		var version int
		var name string

		// Parse filename format: "001_add_workflow_status.sql"
		// Extract version number (first 3 digits) and name (everything after first underscore)
		if len(filename) < 5 || filename[3] != '_' {
			continue
		}

		// Parse version number (first 3 digits)
		if _, err := fmt.Sscanf(filename[:3], "%d", &version); err != nil {
			continue
		}

		// Extract name (everything after first underscore, minus .sql)
		name = filename[4 : len(filename)-4] // Remove "NNN_" prefix and ".sql" suffix

		// Read SQL content from file
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read migration file %s: %w", file, err)
		}

		migrations = append(migrations, Migration{
			Version: version,
			Name:    name,
			SQL:     string(content),
		})
	}

	// Sort migrations by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// getAppliedMigrations returns the list of applied migration versions
func (mr *MigrationRunner) getAppliedMigrations() ([]int, error) {
	query := `SELECT version FROM schema_migrations ORDER BY version`
	rows, err := mr.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []int
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}

	return versions, nil
}

// isMigrationApplied checks if a migration has been applied
func (mr *MigrationRunner) isMigrationApplied(version int, appliedMigrations []int) bool {
	for _, applied := range appliedMigrations {
		if applied == version {
			return true
		}
	}
	return false
}

// runMigration runs a single migration
func (mr *MigrationRunner) runMigration(migration Migration) error {
	// Start transaction
	tx, err := mr.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Execute migration SQL
	_, err = tx.Exec(migration.SQL)
	if err != nil {
		// Check if this is a duplicate column error for migration 006
		if migration.Version == 6 && migration.Name == "add_selected_folders_to_presets" {
			errorStr := err.Error()
			if strings.Contains(errorStr, "duplicate column name") && strings.Contains(errorStr, "selected_folder") {
				// This is expected - the column already exists, so we can safely ignore this error
				fmt.Printf("⚠️  Migration %d: %s - Column 'selected_folder' already exists, skipping\n", migration.Version, migration.Name)

				// Record migration as applied even though we skipped the SQL
				_, recordErr := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, migration.Version)
				if recordErr != nil {
					return fmt.Errorf("failed to record migration: %w", recordErr)
				}

				// Commit transaction
				if err := tx.Commit(); err != nil {
					return fmt.Errorf("failed to commit migration: %w", err)
				}

				fmt.Printf("✅ Applied migration %d: %s (skipped duplicate column)\n", migration.Version, migration.Name)
				return nil
			}
		}
		return fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	// Record migration as applied
	_, err = tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, migration.Version)
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration: %w", err)
	}

	fmt.Printf("✅ Applied migration %d: %s\n", migration.Version, migration.Name)
	return nil
}
