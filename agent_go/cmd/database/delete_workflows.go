package database

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"mcp-agent-builder-go/agent_go/pkg/database"
)

var deleteWorkflowsCmd = &cobra.Command{
	Use:   "delete-workflows",
	Short: "Delete all workflow chat sessions and their events",
	Long: `Delete all chat sessions with agent_mode = 'workflow' and all their associated events.

This command will:
- Delete all chat sessions where agent_mode = 'workflow'
- Automatically delete all events associated with those sessions (via CASCADE)
- Return the count of deleted sessions

Example:
  mcp-agent db delete-workflows --db-path /app/chat_history.db`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get database path from flag or viper
		dbPath := viper.GetString("db.path")
		if dbPath == "" {
			dbPath = "/app/chat_history.db"
		}

		fmt.Printf("Connecting to database: %s\n", dbPath)

		// Initialize database
		db, err := database.NewSQLiteDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		defer db.Close()

		// Delete workflow sessions
		ctx := context.Background()
		count, err := db.DeleteWorkflowSessions(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete workflow sessions: %w", err)
		}

		fmt.Printf("✅ Successfully deleted %d workflow session(s) and their events\n", count)
		return nil
	},
}
