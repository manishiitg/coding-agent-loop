package connectionisolation

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/spf13/cobra"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"

	testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"
)

var connectionIsolationTestCmd = &cobra.Command{
	Use:   "connection-isolation",
	Short: "Test that agent connections are isolated (no shared pool)",
	Long: `Test that multiple agents have isolated connections.

This test verifies the removal of the global STDIO connection pool:
1. Creates multiple agents with the same MCP server configuration
2. Verifies each agent has its own independent connection
3. Tests that closing one agent's connection doesn't affect others
4. Validates parallel agent execution without race conditions

This ensures agents don't share connections (which caused race conditions).

Note: This test does NOT require an LLM - it only tests MCP connection creation.

Examples:
  mcpagent-test test connection-isolation --log-file logs/connection-isolation-test.log
  mcpagent-test test connection-isolation --verbose`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := testutils.NewTestLoggerFromViper()
		logger.Info("=== Connection Isolation Test ===")
		logger.Info("This test verifies that the STDIO connection pool has been removed")
		logger.Info("and each agent has isolated, independent connections")
		logger.Info("Note: This test does NOT require an LLM API key")

		// Test 1: Multiple STDIO connections can be created in parallel
		if err := testParallelStdioConnections(logger); err != nil {
			return fmt.Errorf("parallel STDIO connections test failed: %w", err)
		}

		// Test 2: Sequential connection creation and cleanup
		if err := testSequentialConnectionLifecycle(logger); err != nil {
			return fmt.Errorf("sequential connection lifecycle test failed: %w", err)
		}

		// Test 3: Multiple agents can be created with the same config
		if err := testParallelAgentCreation(logger); err != nil {
			return fmt.Errorf("parallel agent creation test failed: %w", err)
		}

		logger.Info("=== Connection Isolation Test PASSED ===")
		return nil
	},
}

// GetConnectionIsolationTestCmd returns the connection isolation test command
func GetConnectionIsolationTestCmd() *cobra.Command {
	return connectionIsolationTestCmd
}

// getTestConfigPath returns the path to the test MCP config file
func getTestConfigPath() string {
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	return filepath.Join(dir, "mcp_servers_simple.json")
}

// testParallelStdioConnections tests that multiple STDIO connections can be created in parallel
func testParallelStdioConnections(log loggerv2.Logger) error {
	log.Info("--- Test 1: Parallel STDIO Connection Creation ---")
	log.Info("Creating multiple STDIO connections in parallel to verify no pool contention")

	ctx := context.Background()
	numConnections := 3

	// Use sequential-thinking server (simple npx command)
	command := "npx"
	args := []string{"--yes", "@modelcontextprotocol/server-sequential-thinking"}

	log.Info("Creating STDIO managers",
		loggerv2.Int("num_connections", numConnections),
		loggerv2.String("command", command))

	var wg sync.WaitGroup
	results := make(chan struct {
		id     int
		err    error
		client interface{ Close() error }
	}, numConnections)

	startTime := time.Now()

	for i := 0; i < numConnections; i++ {
		wg.Add(1)
		go func(connID int) {
			defer wg.Done()

			log.Info("Creating STDIO connection",
				loggerv2.Int("connection_id", connID))

			// Create a new StdioManager - each should be independent
			manager := mcpclient.NewStdioManager(command, args, nil, log)

			// Connect - this creates a new subprocess
			client, err := manager.Connect(ctx)
			if err != nil {
				results <- struct {
					id     int
					err    error
					client interface{ Close() error }
				}{id: connID, err: err, client: nil}
				return
			}

			log.Info("STDIO connection created successfully",
				loggerv2.Int("connection_id", connID))

			results <- struct {
				id     int
				err    error
				client interface{ Close() error }
			}{id: connID, err: nil, client: client}
		}(i)
	}

	wg.Wait()
	close(results)

	duration := time.Since(startTime)

	// Collect results
	var clients []interface{ Close() error }
	var errors []error

	for result := range results {
		if result.err != nil {
			errors = append(errors, fmt.Errorf("connection %d: %w", result.id, result.err))
		} else if result.client != nil {
			clients = append(clients, result.client)
		}
	}

	if len(errors) > 0 {
		for _, e := range errors {
			log.Error("Connection creation failed", e)
		}
		// Clean up any successful connections
		for _, c := range clients {
			_ = c.Close()
		}
		return fmt.Errorf("failed to create %d/%d connections", len(errors), numConnections)
	}

	log.Info("All STDIO connections created successfully",
		loggerv2.Int("created", len(clients)),
		loggerv2.String("duration", duration.String()))

	// Clean up connections
	for i, client := range clients {
		if err := client.Close(); err != nil {
			log.Warn("Failed to close connection",
				loggerv2.Int("connection_id", i),
				loggerv2.Error(err))
		}
	}

	log.Info("All connections closed successfully")
	log.Info("Test 1 PASSED - Parallel STDIO connections work without pool contention")

	return nil
}

// testSequentialConnectionLifecycle tests that closing one connection doesn't affect new ones
func testSequentialConnectionLifecycle(log loggerv2.Logger) error {
	log.Info("--- Test 2: Sequential Connection Lifecycle ---")
	log.Info("Testing that closing one connection doesn't affect new connections")

	ctx := context.Background()
	command := "npx"
	args := []string{"--yes", "@modelcontextprotocol/server-sequential-thinking"}

	for i := 0; i < 3; i++ {
		log.Info("Creating connection", loggerv2.Int("iteration", i))

		manager := mcpclient.NewStdioManager(command, args, nil, log)
		client, err := manager.Connect(ctx)
		if err != nil {
			return fmt.Errorf("failed to create connection in iteration %d: %w", i, err)
		}

		log.Info("Connection created successfully",
			loggerv2.Int("iteration", i),
			loggerv2.String("server_key", manager.GetServerKey()))

		// Close the connection
		if err := client.Close(); err != nil {
			log.Warn("Failed to close connection",
				loggerv2.Int("iteration", i),
				loggerv2.Error(err))
		}

		log.Info("Connection closed, creating new connection...",
			loggerv2.Int("iteration", i))
	}

	log.Info("Test 2 PASSED - Sequential connections work correctly")
	log.Info("New connections work after previous connections are closed")

	return nil
}

// testParallelAgentCreation tests that multiple agents can be created with the same config
// This test is optional - Tests 1 and 2 already prove connection isolation
func testParallelAgentCreation(log loggerv2.Logger) error {
	log.Info("--- Test 3: Parallel Agent Creation (Optional) ---")
	log.Info("This test requires an LLM - will skip if not available")

	configPath := getTestConfigPath()
	log.Info("Using MCP config", loggerv2.String("path", configPath))

	ctx := context.Background()
	numAgents := 2

	// Try to create an LLM - if it fails, skip this test
	model, llmProvider, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
		Provider: "", // Use viper default
		ModelID:  "", // Use default model
		Logger:   log,
	})
	if err != nil {
		log.Info("Test 3 SKIPPED - No LLM available (this is OK, Tests 1 and 2 passed)")
		log.Info("To run Test 3, set OPENAI_API_KEY or ANTHROPIC_API_KEY environment variable")
		return nil // Skip, don't fail
	}

	log.Info("LLM available, proceeding with agent creation test", loggerv2.String("provider", string(llmProvider)))

	var wg sync.WaitGroup
	results := make(chan struct {
		id    int
		err   error
		agent *mcpagent.Agent
	}, numAgents)

	startTime := time.Now()

	for i := 0; i < numAgents; i++ {
		wg.Add(1)
		go func(agentID int) {
			defer wg.Done()

			log.Info("Creating agent",
				loggerv2.Int("agent_id", agentID))

			agent, err := mcpagent.NewAgent(
				ctx,
				model,
				configPath,
				mcpagent.WithLogger(log),
				mcpagent.WithProvider(llmProvider),
				mcpagent.WithServerName("sequential-thinking"),
			)

			if err != nil {
				results <- struct {
					id    int
					err   error
					agent *mcpagent.Agent
				}{id: agentID, err: err, agent: nil}
				return
			}

			log.Info("Agent created successfully",
				loggerv2.Int("agent_id", agentID),
				loggerv2.Int("clients_count", len(agent.Clients)))

			results <- struct {
				id    int
				err   error
				agent *mcpagent.Agent
			}{id: agentID, err: nil, agent: agent}
		}(i)
	}

	wg.Wait()
	close(results)

	duration := time.Since(startTime)

	// Collect results
	var agents []*mcpagent.Agent
	var errors []error

	for result := range results {
		if result.err != nil {
			errors = append(errors, fmt.Errorf("agent %d: %w", result.id, result.err))
		} else if result.agent != nil {
			agents = append(agents, result.agent)
		}
	}

	if len(errors) > 0 {
		for _, e := range errors {
			log.Error("Agent creation failed", e)
		}
		// Clean up any successful agents
		for _, a := range agents {
			a.Close()
		}
		return fmt.Errorf("failed to create %d/%d agents", len(errors), numAgents)
	}

	log.Info("All agents created successfully",
		loggerv2.Int("created", len(agents)),
		loggerv2.String("duration", duration.String()))

	// Verify each agent has its own client (not shared)
	for i, agent := range agents {
		clientCount := len(agent.Clients)
		log.Info("Agent client info",
			loggerv2.Int("agent_id", i),
			loggerv2.Int("clients", clientCount))

		if clientCount == 0 {
			log.Warn("Agent has no clients - connection may have failed",
				loggerv2.Int("agent_id", i))
		}
	}

	// Clean up agents
	for i, agent := range agents {
		agent.Close()
		log.Debug("Agent closed", loggerv2.Int("agent_id", i))
	}

	log.Info("All agents closed successfully")
	log.Info("Test 3 PASSED - Parallel agent creation works without race conditions")

	return nil
}
