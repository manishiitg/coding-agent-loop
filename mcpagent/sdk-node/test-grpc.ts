/**
 * Simple test script to verify the gRPC implementation works.
 *
 * Run with: npx ts-node test-grpc.ts
 */

import { MCPAgent } from './src';

async function main() {
  console.log('=== MCPAgent gRPC Test ===\n');

  // Create agent client (gRPC enabled by default)
  const agent = new MCPAgent({
    serverOptions: {
      mcpConfigPath: '../examples/nodejs-http/mcp_servers.json',
      logLevel: 'debug',
    },
  });

  try {
    // Step 1: Initialize the agent
    console.log('1. Initializing agent with gRPC...');
    const info = await agent.initialize({
      provider: 'openai',
      modelId: 'gpt-4o-mini',
    });

    console.log(`   Agent ID: ${info.agentId}`);
    console.log(`   Session ID: ${info.sessionId}`);
    console.log(`   Available tools: ${info.capabilities.tools.length}`);
    console.log(`   Connected servers: ${info.capabilities.servers.join(', ')}`);
    console.log();

    // Step 2: Test streaming ask
    console.log('2. Testing askStream("What is 2+2?")...');
    console.log('   Response: ');

    for await (const event of agent.askStream('What is 2+2? Answer briefly.')) {
      if (event.type === 'chunk') {
        process.stdout.write(event.text);
      } else if (event.type === 'final') {
        console.log('\n');
        console.log(`   Duration: ${event.durationMs}ms`);
        console.log(`   Tokens: ${event.tokenUsage.totalTokens}`);
      } else if (event.type === 'error') {
        console.log(`\n   Error: ${event.message}`);
      }
    }
    console.log();

    // Step 3: Test standard ask (uses streaming internally)
    console.log('3. Testing ask("What day is today?")...');
    const response = await agent.ask('What day is today? Answer briefly.');

    console.log(`   Response: ${response.response}`);
    console.log(`   Duration: ${response.durationMs}ms`);
    console.log(`   Tokens: ${response.tokenUsage.totalTokens}`);
    console.log();

    // Step 4: Get token usage
    console.log('4. Getting token usage...');
    const usage = await agent.getTokenUsage();

    console.log(`   Total tokens: ${usage.totalTokens}`);
    console.log(`   LLM calls: ${usage.llmCallCount}`);
    console.log(`   Total cost: $${usage.costs.totalCost.toFixed(6)}`);
    console.log();

  } catch (error) {
    console.error('Error:', error);
  } finally {
    // Cleanup
    console.log('5. Destroying agent...');
    await agent.destroy();
    console.log('   Done!\n');
  }
}

main().catch(console.error);
