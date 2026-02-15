/**
 * Multi-turn Conversation Example
 *
 * This example demonstrates how to have a multi-turn conversation
 * with an MCPAgent, maintaining context across multiple questions.
 *
 * Prerequisites:
 * 1. Set up Vertex AI credentials:
 *    export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
 *    # or configure gcloud CLI authentication
 *
 * 2. Run this example:
 *    npm install
 *    npm run dev:multi-turn
 */

import { MCPAgent, Message } from '@mcpagent/node';
import path from 'path';

async function main() {
  console.log('=== MCPAgent Multi-turn Conversation Example ===\n');

  const agent = new MCPAgent({
    serverOptions: {
      mcpConfigPath: path.join(__dirname, '..', 'mcp_servers.json'),
      logLevel: 'info',
    },
  });

  try {
    // Initialize with MCP servers config
    await agent.initialize({
      provider: 'vertex',
      modelId: 'gemini-3-flash-preview',
      selectedServers: ['context7'],
    });
    console.log(`Agent ready: ${agent.getAgentId()}\n`);

    // Conversation history
    let messages: Message[] = [];

    // Helper function for chatting
    async function chat(userMessage: string): Promise<string> {
      console.log(`You: ${userMessage}\n`);

      // Add user message
      messages.push({ role: 'user', content: userMessage });

      // Get response with full history
      const result = await agent.askWithHistory(messages);

      // Update history
      messages = result.updatedMessages;

      console.log(`Assistant: ${result.response}\n`);
      console.log(`  [${result.tokenUsage.totalTokens} tokens, ${result.durationMs}ms]\n`);
      console.log('---\n');

      return result.response;
    }

    // Multi-turn conversation with responses
    const reply1 = await chat('What MCP servers am I connected to?');
    console.log('Reply 1:', reply1, '\n');

    const reply2 = await chat('Pick one of those servers and tell me what tools it provides.');
    console.log('Reply 2:', reply2, '\n');

    const reply3 = await chat('Can you demonstrate using one of those tools?');
    console.log('Reply 3:', reply3, '\n');

    const reply4 = await chat('Summarize what we discussed in this conversation.');
    console.log('Reply 4:', reply4, '\n');

    // Final stats
    const usage = await agent.getTokenUsage();
    console.log('=== Session Summary ===');
    console.log(`Total tokens: ${usage.totalTokens}`);
    console.log(`LLM calls: ${usage.llmCallCount}`);
    console.log(`Total cost: $${usage.costs.totalCost.toFixed(6)}`);
    console.log(`Messages in history: ${messages.length}`);

  } catch (error) {
    console.error('Error:', error);
  } finally {
    await agent.destroy();
    console.log('\nSession ended.');
  }
}

main();
