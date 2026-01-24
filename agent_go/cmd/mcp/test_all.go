package mcp

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/logger"
	"github.com/manishiitg/mcpagent/mcpclient"

	"github.com/spf13/cobra"
)

var testAllCmd = &cobra.Command{
	Use:   "test-all",
	Short: "Test connections to all MCP servers",
	Long:  "Connect to all configured MCP servers and display their capabilities. Useful for testing MCP server connectivity without starting the full server.",
	Run:   runTestAll,
}

func init() {
	testAllCmd.Flags().String("config", "configs/mcp_servers_clean.json", "MCP servers configuration path")
	testAllCmd.Flags().Duration("timeout", 10*time.Minute, "Connection timeout per server")
}

func runTestAll(cmd *cobra.Command, args []string) {
	// Get config file from command line flag
	configFile, _ := cmd.Flags().GetString("config")
	if configFile == "" {
		configFile = "configs/mcp_servers_clean.json" // Default fallback
	}

	timeout, _ := cmd.Flags().GetDuration("timeout")

	fmt.Printf("🔌 Testing connections to all MCP servers\n")
	fmt.Printf("📁 Config file: %s\n", configFile)
	fmt.Printf("⏱️  Timeout per server: %v\n", timeout)
	fmt.Println("=========================================")

	// Load merged configuration (base + user)
	config, err := mcpclient.LoadMergedConfig(configFile, nil)
	if err != nil {
		log.Fatalf("❌ Failed to load merged config: %v", err)
	}

	servers := config.ListServers()
	if len(servers) == 0 {
		fmt.Println("⚠️  No servers found in configuration")
		return
	}

	fmt.Printf("📋 Found %d server(s) in configuration\n\n", len(servers))

	// Create logger
	v2Logger, err := logger.CreateLogger("", "info", "text", true)
	if err != nil {
		log.Fatalf("❌ Failed to create logger: %v", err)
	}
	defer v2Logger.Close()

	// Track results
	successCount := 0
	failCount := 0
	var failedServers []string

	// Test each server
	for i, serverName := range servers {
		fmt.Printf("\n[%d/%d] Testing: %s\n", i+1, len(servers), serverName)
		fmt.Println("----------------------------------------")

		// Get server configuration
		serverConfig, err := config.GetServer(serverName)
		if err != nil {
			fmt.Printf("❌ Failed to get server config: %v\n", err)
			failCount++
			failedServers = append(failedServers, serverName)
			continue
		}

		// Create client
		client := mcpclient.New(serverConfig, v2Logger)
		defer client.Close()

		// Create context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		// Attempt connection
		startTime := time.Now()
		if err := client.ConnectWithRetry(ctx); err != nil {
			duration := time.Since(startTime)
			fmt.Printf("❌ Connection failed after %v: %v\n", duration.Round(time.Second), err)
			failCount++
			failedServers = append(failedServers, serverName)
			continue
		}

		duration := time.Since(startTime)
		fmt.Printf("✅ Connected successfully in %v\n", duration.Round(time.Millisecond))

		// Show server info
		if serverInfo := client.GetServerInfo(); serverInfo != nil {
			fmt.Printf("   Name: %s\n", serverInfo.Name)
			fmt.Printf("   Version: %s\n", serverInfo.Version)
		}

		// List tools
		tools, err := client.ListTools(ctx)
		if err != nil {
			fmt.Printf("⚠️  Failed to list tools: %v\n", err)
		} else {
			fmt.Printf("   Tools: %d\n", len(tools))
			if len(tools) > 0 && len(tools) <= 5 {
				// Show first few tool names
				for _, tool := range tools[:min(len(tools), 5)] {
					fmt.Printf("     - %s\n", tool.Name)
				}
				if len(tools) > 5 {
					fmt.Printf("     ... and %d more\n", len(tools)-5)
				}
			}
		}

		successCount++
	}

	// Print summary
	fmt.Println("\n=========================================")
	fmt.Printf("📊 Summary:\n")
	fmt.Printf("   ✅ Successful: %d\n", successCount)
	fmt.Printf("   ❌ Failed: %d\n", failCount)
	fmt.Printf("   📋 Total: %d\n", len(servers))

	if failCount > 0 {
		fmt.Printf("\n❌ Failed servers:\n")
		for _, server := range failedServers {
			fmt.Printf("   - %s\n", server)
		}
		os.Exit(1)
	} else {
		fmt.Println("\n🎉 All servers connected successfully!")
		os.Exit(0)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
