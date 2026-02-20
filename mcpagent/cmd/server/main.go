package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/manishiitg/mcpagent/grpcserver"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

func main() {
	// Parse command line flags
	socketPath := flag.String("socket", "", "gRPC Unix domain socket path (required)")
	configPath := flag.String("config", "mcp_servers.json", "Path to MCP servers configuration file")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	parentPID := flag.Int("parent-pid", 0, "Parent process ID to monitor (exit when parent dies)")
	flag.Parse()

	if *socketPath == "" {
		fmt.Fprintf(os.Stderr, "Error: --socket flag is required\n")
		os.Exit(1)
	}

	// Initialize logger
	logger, err := loggerv2.New(loggerv2.Config{
		Level:  *logLevel,
		Format: "text",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	// Create gRPC server
	server := grpcserver.NewServer(grpcserver.Config{
		SocketPath:        *socketPath,
		DefaultConfigPath: *configPath,
		Logger:            logger,
	})

	// Handle graceful shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Monitor parent process if specified
	if *parentPID > 0 {
		go func() {
			for {
				time.Sleep(1 * time.Second)
				// Check if parent process is still alive
				proc, err := os.FindProcess(*parentPID)
				if err != nil {
					logger.Info("Parent process not found, shutting down",
						loggerv2.Int("parent_pid", *parentPID))
					shutdown <- syscall.SIGTERM
					return
				}
				// On Unix, sending signal 0 checks if process exists
				if err := proc.Signal(syscall.Signal(0)); err != nil {
					logger.Info("Parent process died, shutting down",
						loggerv2.Int("parent_pid", *parentPID))
					shutdown <- syscall.SIGTERM
					return
				}
			}
		}()
	}

	// Start gRPC server in goroutine
	go func() {
		logger.Info("MCPAgent gRPC Server starting",
			loggerv2.String("socket", *socketPath),
			loggerv2.String("config", *configPath),
		)
		fmt.Printf("\n  MCPAgent Server\n")
		fmt.Printf("  ===============\n")
		fmt.Printf("  gRPC Socket: %s\n", *socketPath)
		fmt.Printf("  Config: %s\n", *configPath)
		fmt.Printf("\n  gRPC Services:\n")
		fmt.Printf("    AgentService.CreateAgent           - Create agent\n")
		fmt.Printf("    AgentService.GetAgent              - Get agent info\n")
		fmt.Printf("    AgentService.ListAgents            - List agents\n")
		fmt.Printf("    AgentService.DestroyAgent          - Destroy agent\n")
		fmt.Printf("    AgentService.Ask                   - Ask question (unary)\n")
		fmt.Printf("    AgentService.AskWithHistory        - Multi-turn (unary)\n")
		fmt.Printf("    AgentService.Converse              - Bidirectional streaming\n")
		fmt.Printf("    AgentService.GetTokenUsage         - Token stats\n")
		fmt.Printf("    AgentService.HealthCheck           - Health check\n")
		fmt.Printf("\n  Ready to accept connections...\n\n")

		if err := server.Start(); err != nil {
			logger.Error("Server error", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	<-shutdown
	logger.Info("Shutdown signal received")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Shutdown error", err)
		os.Exit(1)
	}

	logger.Info("Server stopped gracefully")
}
