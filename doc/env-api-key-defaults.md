# Environment-Based API Key Defaults

Set API keys via environment variables to auto-populate the frontend LLM configuration.

## Supported Variables

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `ANTHROPIC_PRIMARY_MODEL` | Default model (defaults to `claude-sonnet-4-20250514`) |
| `AZURE_AI_API_KEY` | Azure OpenAI API key |
| `AZURE_AI_ENDPOINT` | Azure OpenAI endpoint URL |
| `AZURE_PRIMARY_MODEL` | Default Azure model |
| `OPENROUTER_API_KEY` | OpenRouter API key |
| `OPENAI_API_KEY` | OpenAI API key |

## Local Development

Add to `agent_go/.env`:
```bash
ANTHROPIC_API_KEY=sk-ant-...
```

## Azure Deployment

Pass via Terraform variable:
```bash
export TF_VAR_anthropic_api_key="sk-ant-..."
terraform apply
```

Or in `terraform.tfvars` (do not commit):
```hcl
anthropic_api_key = "sk-ant-..."
```

## How It Works

The `/api/llm-config/defaults` endpoint returns configs with API keys from environment variables. The frontend uses these to pre-fill the LLM Configuration modal when no user-saved value exists.
