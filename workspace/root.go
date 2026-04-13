package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "planner",
	Short: "Planner REST API - Markdown Document Management",
	Long: `A REST API focused on markdown document management with advanced patching capabilities and GitHub integration.

This tool provides:
- Markdown document CRUD operations
- Structure analysis and parsing
- GitHub version control integration
- LLM agent ready endpoints`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.planner.yaml)")
	rootCmd.PersistentFlags().String("port", "8080", "HTTP server port")
	rootCmd.PersistentFlags().String("docs-dir", "./workspace-docs", "Documents directory")
	rootCmd.PersistentFlags().String("github-token", "", "GitHub personal access token")
	rootCmd.PersistentFlags().String("github-repo", "", "GitHub repository (username/repo-name)")

	// Bind flags to viper
	viper.BindPFlag("port", rootCmd.PersistentFlags().Lookup("port"))
	viper.BindPFlag("docs-dir", rootCmd.PersistentFlags().Lookup("docs-dir"))
	viper.BindPFlag("github-token", rootCmd.PersistentFlags().Lookup("github-token"))
	viper.BindPFlag("github-repo", rootCmd.PersistentFlags().Lookup("github-repo"))

	// Set environment variable key replacer for Viper
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	// Set default values
	viper.SetDefault("github-branch", "main")

	// Bind environment variables with correct prefixes
	viper.BindEnv("github-branch", "WORKSPACE_GITHUB_BRANCH")
	viper.BindEnv("github-token", "GITHUB_TOKEN")
	viper.BindEnv("github-repo", "GITHUB_REPO")
	viper.BindEnv("docs-dir", "DOCS_DIR")
	viper.BindEnv("enable-github-sync", "WORKSPACE_ENABLE_GITHUB_SYNC")

	// Add subcommands
	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(syncCmd)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".planner" (without extension).
		viper.AddConfigPath(home)
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName(".planner")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
