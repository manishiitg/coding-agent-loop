package pulsestore

import (
	"context"
	"io/fs"
	"path/filepath"
)

// MigrateWorkspaceDatabases performs the deployment-time pass. Lazy Ensure
// remains mandatory because another laptop may not have been online during it.
func MigrateWorkspaceDatabases(ctx context.Context, root string) (BatchResult, error) {
	var report BatchResult
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" || name == ".backups" || name == "backup" {
				return filepath.SkipDir
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			first := relative
			if separator := len(relative); separator > 0 {
				if index := indexPathSeparator(relative); index >= 0 {
					first = relative[:index]
				}
			}
			// These roots are leaked absolute/temp paths created by old tests or
			// path-normalization bugs, not workspace records. Never mutate them in
			// a deployment migration.
			if first == "var" || first == "Users" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "db.sqlite" || filepath.Base(filepath.Dir(path)) != "db" {
			return nil
		}
		report.Scanned++
		result, err := MigratePath(ctx, path)
		if err != nil {
			report.Failures = append(report.Failures, BatchFailure{Path: path, Error: err.Error()})
			return nil
		}
		switch {
		case result.Applied:
			report.Migrated++
			report.Results = append(report.Results, result)
		case result.AlreadyCurrent:
			report.Current++
		default:
			report.Skipped++
		}
		return nil
	})
	return report, err
}

func indexPathSeparator(path string) int {
	for index, character := range path {
		if character == '/' || character == '\\' {
			return index
		}
	}
	return -1
}
