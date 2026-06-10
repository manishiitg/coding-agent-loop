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
	Long: `A REST API focused on markdown document management with advanced patching capabilities.

This tool provides:
- Markdown document CRUD operations
- Structure analysis and parsing
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
	rootCmd.PersistentFlags().String("host", "", "Bind address (empty = all interfaces; set 127.0.0.1 to restrict to localhost)")
	rootCmd.PersistentFlags().String("docs-dir", "./workspace-docs", "Documents directory")

	// Bind flags to viper
	viper.BindPFlag("port", rootCmd.PersistentFlags().Lookup("port"))
	viper.BindPFlag("host", rootCmd.PersistentFlags().Lookup("host"))
	viper.BindPFlag("docs-dir", rootCmd.PersistentFlags().Lookup("docs-dir"))

	// Set environment variable key replacer for Viper
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	// Bind environment variables with correct prefixes
	viper.BindEnv("docs-dir", "DOCS_DIR")
	viper.BindEnv("host", "BIND_HOST")

	// Add subcommands
	rootCmd.AddCommand(serverCmd)
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
