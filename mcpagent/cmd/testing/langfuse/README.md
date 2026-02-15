# Langfuse Testing

## Prerequisites

- Ensure `.env` file exists in the `mcpagent/` directory with:
  - `LANGFUSE_PUBLIC_KEY` - Your Langfuse public key
  - `LANGFUSE_SECRET_KEY` - Your Langfuse secret key
  - `LANGFUSE_BASE_URL` - Langfuse host (optional, defaults to https://cloud.langfuse.com)
  - `OPENAI_API_KEY` - OpenAI API key (required for `--provider openai`)
  - AWS credentials (required for `--provider bedrock`)

## Workflow

**Note**: Do not build binaries. Use `go run` to execute tests directly.

1. **Run agent-mcp test** to create a trace (Langfuse is automatically used if credentials are available):
   ```bash
   cd mcpagent
   go run ./cmd/testing agent-mcp --provider openai --model gpt-4.1-mini
   ```

2. **Read the trace** to verify it was created properly:
   ```bash
   cd mcpagent
   go run ./cmd/testing langfuse-read --trace-id <trace-id> --observations
   ```

The agent-mcp test will output a `trace_id` in the logs - use that to read the trace and verify token usage and output are present.

## Alternative: Using Existing Binary

If a binary already exists (e.g., `mcpagent-test` in parent directory):
```bash
# From mcpagent/ directory
../mcpagent-test agent-mcp --provider openai --model gpt-4.1-mini
../mcpagent-test langfuse-read --trace-id <trace-id> --observations
```

**Important**: Do not create new binaries. Always use `go run` for testing.

