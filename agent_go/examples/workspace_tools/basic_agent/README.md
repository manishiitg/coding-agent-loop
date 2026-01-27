# Basic Agent Example

This example demonstrates a simple AI agent that uses the **Workspace Basic Tools** (file listing, reading, writing) to interact with a local file system via the `workspace-api`.

## Prerequisites

1.  **Go 1.22+**
2.  **Docker & Docker Compose**
3.  **Google Cloud Vertex AI Access**: You need a valid `GOOGLE_APPLICATION_CREDENTIALS` JSON key file or be authenticated via `gcloud auth application-default login`.

## Setup

1.  **Prepare Test Data**:
    The `test-docs` folder is mounted into the workspace container. Any files you put here will be visible to the agent.
    ```bash
    mkdir -p test-docs
    echo "Hello from the test file!" > test-docs/hello.txt
    ```

2.  **Start the Workspace API**:
    Run the workspace API service using Docker Compose. This service provides the REST API that the agent talks to.
    ```bash
    docker-compose up -d
    ```
    The API will be available at `http://localhost:8083`.

## Running the Agent

1.  **Set Environment Variables**:
    Set your Google Cloud project details.
    ```bash
    export GOOGLE_CLOUD_PROJECT="your-project-id"
    export GOOGLE_CLOUD_LOCATION="us-central1"
    # export GOOGLE_APPLICATION_CREDENTIALS="/path/to/key.json" # If not using default auth
    ```

2.  **Run the Agent**:
    ```bash
    go run main.go
    ```

## How it Works

1.  **Initialization**: The agent connects to `http://localhost:8083`.
2.  **Tool Loading**: It loads the `workspace_basic` tool definitions (List, Read, Write, etc.) from `pkg/workspace`.
3.  **Prompt**: The agent is sent a prompt: *"list all files in the workspace and tell me what you see"*.
4.  **Tool Execution**: 
    *   The LLM decides to call `list_workspace_files`.
    *   The agent executes this tool against the `workspace-api`.
    *   The results (JSON listing of `test-docs`) are sent back to the LLM.
5.  **Final Response**: The LLM summarizes the file listing in natural language.

## Cleanup

Stop the workspace container:
```bash
docker-compose down
```
