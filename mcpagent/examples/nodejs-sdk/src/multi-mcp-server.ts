/**
 * Multi-MCP Server Example
 *
 * This example demonstrates how to use multiple MCP servers simultaneously,
 * allowing the agent to leverage different tools from different servers
 * in a single conversation.
 *
 * Prerequisites:
 * 1. Set up Vertex AI credentials:
 *    export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
 *    # or configure gcloud CLI authentication
 *
 * 2. Run this example:
 *    npm install
 *    npm run dev:multi-mcp-server
 */

import { MCPAgent, Message } from '@mcpagent/node';
import path from 'path';

async function main() {
  console.log('=== MCPAgent Multi-MCP Server Example ===\n');

  const agent = new MCPAgent({
    serverOptions: {
      mcpConfigPath: path.join(__dirname, '..', 'mcp_servers.json'),
      logLevel: 'info',
    },
  });

  try {
    // Initialize with multiple MCP servers
    // The agent will have access to tools from all selected servers
    await agent.initialize({
      provider: 'vertex',
      modelId: 'gemini-3-flash-preview',
      // Select multiple servers to use together
      selectedServers: ['context7', 'sequential-thinking', 'ddg-search'],
    });

    console.log(`Agent ready: ${agent.getAgentId()}`);

    // Get capabilities to see available tools from all servers
    const capabilities = agent.getCapabilities();
    const servers = capabilities?.servers ?? [];
    const tools = capabilities?.tools ?? [];
    console.log(`Connected servers: ${servers.join(', ')}`);
    console.log(`Available tools: ${tools.length} tools\n`);

    // Conversation history for multi-turn
    let messages: Message[] = [];

    // Helper function for chatting
    async function chat(userMessage: string): Promise<string> {
      console.log(`You: ${userMessage}\n`);

      messages.push({ role: 'user', content: userMessage });

      const result = await agent.askWithHistory(messages);
      messages = result.updatedMessages;

      console.log(`Assistant: ${result.response}\n`);
      console.log(`  [${result.tokenUsage.totalTokens} tokens, ${result.durationMs}ms]`);
      console.log('---\n');

      return result.response;
    }

    // Example 1: Ask about available tools from each server
    await chat('What MCP servers are you connected to and what tools does each provide?');
    console.log('Reply 1 received.\n');

    // Example 2: Use sequential thinking for a complex problem
    await chat(
      'Use sequential thinking to break down this problem: How would you design a REST API for a todo list application?'
    );
    console.log('Reply 2 received.\n');

    // Example 3: Use context7 for documentation lookup
    await chat(
      'Can you look up documentation for Express.js using context7 and tell me how to set up basic routing?'
    );
    console.log('Reply 3 received.\n');

    // Example 4: Combined task using multiple servers
    await chat(
      'Summarize what we discussed and the tools we used from different servers.'
    );
    console.log('Reply 4 received.\n');

    // Final stats
    const usage = await agent.getTokenUsage();
    console.log('=== Session Summary ===');
    console.log(`Total tokens: ${usage.totalTokens}`);
    console.log(`LLM calls: ${usage.llmCallCount}`);
    console.log(`Total cost: $${usage.costs.totalCost.toFixed(6)}`);
    console.log(`Messages in history: ${messages.length}`);
    console.log(`Servers used: ${servers.join(', ')}`);

  } catch (error) {
    console.error('Error:', error);
  } finally {
    await agent.destroy();
    console.log('\nSession ended.');
  }
}

main();
