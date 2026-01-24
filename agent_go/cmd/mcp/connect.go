package mcp

import (
	"context"
	"fmt"
	"log"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/logger"
	"github.com/manishiitg/mcpagent/mcpclient"

	"github.com/spf13/cobra"
)

var connectCmd = &cobra.Command{
	Use:   "connect <server-name>",
	Short: "Connect to an MCP server and show capabilities",
	Args:  cobra.ExactArgs(1),
	Run:   runConnect,
}

func runConnect(cmd *cobra.Command, args []string) {
	serverName := args[0]
	fmt.Printf("🔌 Connecting to MCP server: %s\n", serverName)

	// Get config file from command line flag
	configFile, _ := cmd.Flags().GetString("config")
	if configFile == "" {
		configFile = "configs/mcp_servers.json" // Default fallback
	}

	// Load merged configuration (base + user)
	config, err := mcpclient.LoadMergedConfig(configFile, nil)
	if err != nil {
		log.Fatalf("Failed to load merged config: %v", err)
	}

	// Get server configuration
	serverConfig, err := config.GetServer(serverName)
	if err != nil {
		log.Fatalf("Server error: %v", err)
	}

	// Use direct connection instead of pooling
	v2Logger, err := logger.CreateLogger("", "info", "text", true)
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer v2Logger.Close()

	client := mcpclient.New(serverConfig, v2Logger)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.ConnectWithRetry(ctx); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	// Show server info
	if serverInfo := client.GetServerInfo(); serverInfo != nil {
		fmt.Printf("✅ Connected to: %s v%s\n", serverInfo.Name, serverInfo.Version)
	}

	// List and display tools
	tools, err := client.ListTools(ctx)
	if err != nil {
		fmt.Printf("⚠️ Failed to list tools: %v\n", err)
	} else {
		// Use the same logger for CLI output
		mcpclient.PrintTools(tools, v2Logger)
	}
}
