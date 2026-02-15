/**
 * Custom Tools Example: Register JavaScript handlers that the LLM can call
 *
 * This example demonstrates how to define custom tools with JavaScript handlers
 * that are executed when the LLM decides to use them during conversations.
 *
 * The Go server is automatically started - no manual setup required!
 *
 * Run this example: npx ts-node examples/custom-tools.ts
 */

import { MCPAgent } from '@mcpagent/node';
import path from 'path';

async function main() {
  // Go server auto-starts on initialize()
  const agent = new MCPAgent({
    serverOptions: {
      mcpConfigPath: path.join(__dirname, '..', 'mcp_servers.json'),
      logLevel: 'info',
    },
  });

  try {
    console.log('Registering custom tools...');

    // Register a calculator tool
    agent.registerTool(
      'calculate',
      'Perform a mathematical calculation. Supports basic arithmetic operations.',
      {
        type: 'object',
        properties: {
          expression: {
            type: 'string',
            description: 'Mathematical expression to evaluate (e.g., "2 + 2", "10 * 5")',
          },
        },
        required: ['expression'],
      },
      async (args) => {
        const expression = args.expression as string;
        console.log(`  [calculate] Evaluating: ${expression}`);

        // Simple safe evaluation (in production, use a proper math parser)
        try {
          // Only allow numbers and basic operators
          if (!/^[\d\s+\-*/().]+$/.test(expression)) {
            throw new Error('Invalid characters in expression');
          }
          const result = Function(`"use strict"; return (${expression})`)();
          return String(result);
        } catch (e) {
          throw new Error(`Failed to evaluate: ${(e as Error).message}`);
        }
      },
      { timeoutMs: 5000 }
    );

    // Register a random number generator
    agent.registerTool(
      'random_number',
      'Generate a random number within a specified range',
      {
        type: 'object',
        properties: {
          min: {
            type: 'number',
            description: 'Minimum value (inclusive)',
          },
          max: {
            type: 'number',
            description: 'Maximum value (inclusive)',
          },
        },
        required: ['min', 'max'],
      },
      async (args) => {
        const min = args.min as number;
        const max = args.max as number;
        const result = Math.floor(Math.random() * (max - min + 1)) + min;
        console.log(`  [random_number] Generated: ${result} (range: ${min}-${max})`);
        return String(result);
      },
      { timeoutMs: 5000 }
    );

    // Register a string manipulation tool
    agent.registerTool(
      'string_utils',
      'Perform string operations like uppercase, lowercase, reverse, or count characters',
      {
        type: 'object',
        properties: {
          text: {
            type: 'string',
            description: 'The text to process',
          },
          operation: {
            type: 'string',
            description: 'Operation to perform: uppercase, lowercase, reverse, length, word_count',
          },
        },
        required: ['text', 'operation'],
      },
      async (args) => {
        const text = args.text as string;
        const operation = args.operation as string;
        console.log(`  [string_utils] Operation: ${operation} on "${text.substring(0, 20)}..."`);

        switch (operation) {
          case 'uppercase':
            return text.toUpperCase();
          case 'lowercase':
            return text.toLowerCase();
          case 'reverse':
            return text.split('').reverse().join('');
          case 'length':
            return String(text.length);
          case 'word_count':
            return String(text.split(/\s+/).filter((w) => w.length > 0).length);
          default:
            throw new Error(`Unknown operation: ${operation}`);
        }
      },
      { timeoutMs: 5000 }
    );

    // Register a current time tool
    agent.registerTool(
      'current_time',
      'Get the current date and time',
      {
        type: 'object',
        properties: {
          format: {
            type: 'string',
            description: 'Format: "iso" for ISO format, "readable" for human-readable',
          },
        },
        required: [],
      },
      async (args) => {
        const format = (args.format as string) || 'readable';
        const now = new Date();
        console.log(`  [current_time] Requested format: ${format}`);

        if (format === 'iso') {
          return now.toISOString();
        }
        return now.toLocaleString();
      },
      { timeoutMs: 5000 }
    );

    console.log('Initializing agent...');

    // Initialize - this starts the callback server automatically
    const info = await agent.initialize({
      provider: 'vertex',
      modelId: 'gemini-3-flash-preview',
    });

    console.log(`Agent created: ${info.agentId}`);
    console.log(`Available tools: ${info.capabilities.tools.join(', ')}`);
    console.log();

    // Example 1: Use the calculator
    console.log('--- Example 1: Calculator ---');
    const calcResponse = await agent.ask('What is 15 * 7 + 23?');
    console.log(`Response: ${calcResponse.response}\n`);

    // Example 2: Use random number
    console.log('--- Example 2: Random Number ---');
    const randomResponse = await agent.ask('Generate a random number between 1 and 100');
    console.log(`Response: ${randomResponse.response}\n`);

    // Example 3: Use string utils
    console.log('--- Example 3: String Utils ---');
    const stringResponse = await agent.ask(
      'How many words are in this sentence: "The quick brown fox jumps over the lazy dog"'
    );
    console.log(`Response: ${stringResponse.response}\n`);

    // Example 4: Multiple tools in one question
    console.log('--- Example 4: Combined ---');
    const combinedResponse = await agent.ask(
      'What time is it right now? Also, what is 123 * 456?'
    );
    console.log(`Response: ${combinedResponse.response}\n`);

    // Show token usage
    const usage = await agent.getTokenUsage();
    console.log('--- Token Usage ---');
    console.log(`Total tokens: ${usage.totalTokens}`);
    console.log(`LLM calls: ${usage.llmCallCount}`);
    console.log(`Total cost: $${usage.costs.totalCost.toFixed(6)}`);
  } catch (error) {
    console.error('Error:', error);
  } finally {
    // Cleanup - this stops the callback server
    await agent.destroy();
    console.log('\nAgent destroyed.');
  }
}

main();
