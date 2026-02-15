/**
 * Basic MCPAgent Node.js SDK Example
 *
 * This example demonstrates how to use the MCPAgent SDK with gRPC streaming.
 * The Go server is automatically started and managed by the SDK.
 *
 * Prerequisites:
 * 1. Set up your .env file with API keys:
 *    OPENAI_API_KEY=your-key
 *
 * 2. Run this example:
 *    npm install
 *    npm run dev
 */

import { MCPAgent } from '@mcpagent/node';
import path from 'path';

async function main() {
  console.log('=== MCPAgent Node.js SDK Example ===\n');

  // Create agent client - Go server auto-starts on initialize()
  const agent = new MCPAgent({
    serverOptions: {
      mcpConfigPath: path.join(__dirname, '..', 'mcp_servers.json'),
      logLevel: 'info',
    },
  });

  try {
    // Step 1: Initialize the agent (this auto-starts the Go server)
    console.log('1. Initializing agent...');
    const info = await agent.initialize({
      provider: 'vertex',
      modelId: 'gemini-3-flash-preview',
    });

    console.log(`   Agent ID: ${info.agentId}`);
    console.log(`   Session ID: ${info.sessionId}`);
    console.log(`   Available tools: ${info.capabilities.tools.length}`);
    console.log(`   Connected servers: ${info.capabilities.servers.join(', ') || 'none'}`);
    console.log();

    // Step 2: Ask a simple question with streaming
    console.log('2. Asking with streaming: "What tools do you have available?"');
    console.log('   Response: ');

    for await (const event of agent.askStream('What tools do you have available? List them briefly.')) {
      if (event.type === 'chunk') {
        process.stdout.write(event.text);
      } else if (event.type === 'final') {
        console.log('\n');
        console.log(`   Duration: ${event.durationMs}ms`);
        console.log(`   Tokens: ${event.tokenUsage.totalTokens}`);
      }
    }
    console.log();

    // Step 3: Ask another question (standard ask, uses streaming internally)
    console.log('3. Asking: "What is 2 + 2?"');
    const response = await agent.ask('What is 2 + 2? Just give the number.');

    console.log(`   Response: ${response.response}`);
    console.log(`   Duration: ${response.durationMs}ms`);
    console.log(`   Tokens: ${response.tokenUsage.totalTokens}`);
    console.log();

    // Step 4: Get cumulative token usage
    console.log('4. Getting token usage summary...');
    const usage = await agent.getTokenUsage();

    console.log(`   Total tokens: ${usage.totalTokens}`);
    console.log(`   LLM calls: ${usage.llmCallCount}`);
    console.log(`   Total cost: $${usage.costs.totalCost.toFixed(6)}`);
    console.log();

  } catch (error) {
    console.error('Error:', error);
  } finally {
    // Step 5: Cleanup (destroys agent and stops Go server)
    console.log('5. Destroying agent...');
    await agent.destroy();
    console.log('   Done!\n');
  }
}

main();
