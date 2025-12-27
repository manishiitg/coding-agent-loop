package database

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// DatabaseCmd represents the database command group
var DatabaseCmd = &cobra.Command{
	Use:   "db",
	Short: "Database management commands",
	Long: `Database management commands for chat history.

Commands:
  delete-workflows  Delete all workflow chat sessions and their events from the database`,
}

func init() {
	// Add database path flag
	DatabaseCmd.PersistentFlags().String("db-path", "/app/chat_history.db", "SQLite database path for chat history")
	viper.BindPFlag("db.path", DatabaseCmd.PersistentFlags().Lookup("db-path"))

	// Add subcommands
	DatabaseCmd.AddCommand(deleteWorkflowsCmd)
}
