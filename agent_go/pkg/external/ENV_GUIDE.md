# Environment Variables Guide for MCP Agent External Package

## 🔧 Required Environment Variables

The external package requires specific environment variables to function properly. Here's a comprehensive guide to all the required and optional variables.

## 🚀 Quick Setup

### For AWS Bedrock (Recommended)
```bash
# Required AWS credentials
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=your_access_key_here
export AWS_SECRET_ACCESS_KEY=your_secret_key_here

# Optional: Set default model
export BEDROCK_PRIMARY_MODEL=anthropic.claude-3-sonnet-20240229-v1:0
```

### For OpenAI
```bash
# Required OpenAI API key
export OPENAI_API_KEY=your_openai_api_key_here
```

## 📋 Detailed Environment Variables

### 🔑 AWS Bedrock Configuration

| Variable | Required | Description | Example |
|----------|----------|-------------|---------|
| `AWS_REGION` | ✅ | AWS region for Bedrock service | `us-east-1` |
| `AWS_ACCESS_KEY_ID` | ✅ | AWS access key ID | `AKIAXXXXXXXXXXXXXXXX` |
| `AWS_SECRET_ACCESS_KEY` | ✅ | AWS secret access key | `xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx` |
| `BEDROCK_PRIMARY_MODEL` | ❌ | Default Bedrock model ID | `anthropic.claude-3-sonnet-20240229-v1:0` |

**Available Bedrock Models:**
- `anthropic.claude-3-sonnet-20240229-v1:0` (Recommended)
- `anthropic.claude-3-haiku-20240307-v1:0`
- `anthropic.claude-3-opus-20240229-v1:0`
- `amazon.titan-text-express-v1`
- `meta.llama2-13b-chat-v1`

### 🤖 OpenAI Configuration

| Variable | Required | Description | Example |
|----------|----------|-------------|---------|
| `OPENAI_API_KEY` | ✅ | OpenAI API key | `sk-...` |

**Available OpenAI Models:**
- `gpt-4o`
- `gpt-4o-mini`
- `gpt-4`
- `gpt-3.5-turbo`

### 📊 Observability Configuration (Optional)

| Variable | Required | Description | Example |
|----------|----------|-------------|---------|
| `TRACING_PROVIDER` | ❌ | Tracing provider | `console`, `langfuse`, `noop` |
| `LANGFUSE_PUBLIC_KEY` | ❌ | Langfuse public key | `pk-...` |
| `LANGFUSE_SECRET_KEY` | ❌ | Langfuse secret key | `sk-...` |
| `LANGFUSE_HOST` | ❌ | Langfuse host URL | `https://cloud.langfuse.com` |

## 🛠️ Setup Examples

### 1. AWS Bedrock Setup

Create a `.env` file in your project root:

```bash
# AWS Credentials
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=your_access_key_here
AWS_SECRET_ACCESS_KEY=your_secret_key_here

# Optional: Default model
BEDROCK_PRIMARY_MODEL=anthropic.claude-3-sonnet-20240229-v1:0

# Optional: Observability
TRACING_PROVIDER=console
```

### 2. OpenAI Setup

```bash
# OpenAI API Key
OPENAI_API_KEY=sk-your-openai-api-key-here

# Optional: Observability
TRACING_PROVIDER=console
```

### 3. Full Setup with Langfuse

```bash
# AWS Credentials
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=your_access_key_here
AWS_SECRET_ACCESS_KEY=your_secret_key_here

# Default model
BEDROCK_PRIMARY_MODEL=anthropic.claude-3-sonnet-20240229-v1:0

# Langfuse Observability
TRACING_PROVIDER=langfuse
LANGFUSE_PUBLIC_KEY=pk-your-public-key
LANGFUSE_SECRET_KEY=sk-your-secret-key
LANGFUSE_HOST=https://cloud.langfuse.com
```

## 🔍 Environment Variable Validation

The external package validates environment variables and provides clear error messages:

### AWS Bedrock Validation
```go
// The package checks for:
if accessKeyID == "" || secretAccessKey == "" {
    return nil, fmt.Errorf("AWS credentials not found in environment variables")
}
```

### OpenAI Validation
```go
// The package checks for:
if apiKey == "" {
    return nil, fmt.Errorf("OPENAI_API_KEY not found in environment variables")
}
```

## 🚨 Common Issues and Solutions

### Issue: "AWS credentials not found"
**Solution:** Set the required AWS environment variables:
```bash
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=your_key
export AWS_SECRET_ACCESS_KEY=your_secret
```

### Issue: "OPENAI_API_KEY not found"
**Solution:** Set your OpenAI API key:
```bash
export OPENAI_API_KEY=sk-your-key-here
```

### Issue: "Failed to create Bedrock LLM"
**Solution:** Check your AWS credentials and region:
```bash
# Verify credentials work
aws sts get-caller-identity
```

### Issue: "Failed to create OpenAI LLM"
**Solution:** Check your OpenAI API key:
```bash
# Test API key
curl -H "Authorization: Bearer $OPENAI_API_KEY" https://api.openai.com/v1/models
```

## 🔐 Security Best Practices

### 1. Never Commit Credentials
```bash
# Add to .gitignore
echo ".env" >> .gitignore
echo "*.key" >> .gitignore
```

### 2. Use Environment-Specific Files
```bash
# Development
.env.development

# Production
.env.production

# Testing
.env.test
```

### 3. Use Secret Management
```bash
# For production, use AWS Secrets Manager or similar
aws secretsmanager get-secret-value --secret-id your-secret-name
```

## 📝 Code Examples

### Loading Environment Variables in Go

```go
package main

import (
    "log"
    "os"
    
    "github.com/joho/godotenv"
)

func main() {
    // Load .env file
    if err := godotenv.Load(); err != nil {
        log.Println("No .env file found, using system environment")
    }
    
    // Verify required variables
    if os.Getenv("AWS_ACCESS_KEY_ID") == "" {
        log.Fatal("AWS_ACCESS_KEY_ID is required")
    }
    
    if os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
        log.Fatal("AWS_SECRET_ACCESS_KEY is required")
    }
    
    // Use the external package
    // ... your agent code here
}
```

### Docker Environment Setup

```dockerfile
# Dockerfile
FROM golang:1.21-alpine

# Copy your application
COPY . /app
WORKDIR /app

# Build the application
RUN go build -o main .

# Set environment variables
ENV AWS_REGION=us-east-1
ENV TRACING_PROVIDER=console

# Run the application
CMD ["./main"]
```

```yaml
# docker-compose.yml
version: '3.8'
services:
  mcp-agent:
    build: .
    environment:
      - AWS_REGION=${AWS_REGION}
      - AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID}
      - AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}
      - BEDROCK_PRIMARY_MODEL=${BEDROCK_PRIMARY_MODEL}
      - TRACING_PROVIDER=${TRACING_PROVIDER}
    env_file:
      - .env
```

## 🧪 Testing Environment Setup

### Test Environment Variables
```bash
# Create test environment
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=test-key
export AWS_SECRET_ACCESS_KEY=test-secret
export BEDROCK_PRIMARY_MODEL=anthropic.claude-3-sonnet-20240229-v1:0
export TRACING_PROVIDER=noop
```

### Running Tests
```bash
# Run tests with environment variables
AWS_REGION=us-east-1 go test ./pkg/external/...
```

## 📊 Environment Variable Reference

### Required Variables by Provider

| Provider | Required Variables | Optional Variables |
|----------|-------------------|-------------------|
| **AWS Bedrock** | `AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | `BEDROCK_PRIMARY_MODEL` |
| **OpenAI** | `OPENAI_API_KEY` | None |

### Observability Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `TRACING_PROVIDER` | `console` | `console`, `langfuse`, `noop` |
| `LANGFUSE_PUBLIC_KEY` | None | Required if using Langfuse |
| `LANGFUSE_SECRET_KEY` | None | Required if using Langfuse |
| `LANGFUSE_HOST` | `https://cloud.langfuse.com` | Langfuse host URL |

## ✅ Verification Commands

### Verify AWS Setup
```bash
# Check AWS credentials
aws sts get-caller-identity

# Test Bedrock access
aws bedrock list-foundation-models --region us-east-1
```

### Verify OpenAI Setup
```bash
# Test OpenAI API
curl -H "Authorization: Bearer $OPENAI_API_KEY" https://api.openai.com/v1/models
```

### Verify Environment Variables
```bash
# Check all variables
env | grep -E "(AWS_|OPENAI_|BEDROCK_|TRACING_|LANGFUSE_)"
```

This guide ensures you have all the necessary environment variables set up correctly for using the MCP Agent External Package. 