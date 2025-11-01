# External Agent Structured Output Test

This test demonstrates the structured output capabilities of the external agent package, allowing you to generate structured JSON responses from LLM conversations.

## 🎯 **Purpose**

The structured output test validates that the external agent can:
- Convert natural language responses to structured JSON format
- Support custom JSON schemas defined by users
- Handle different types of structured data (TodoList, Project, Financial Report)
- Maintain conversation history while generating structured output
- Work with both `AskStructured` and `AskWithHistoryStructured` functions

## 🏗️ **Architecture**

### **Test Structure**
- **TodoList**: Simple task management with nested subtasks and dependencies
- **Project**: Project planning with team members and milestones
- **Financial Report**: Financial data with numeric metrics and key ratios
- **Conversation History**: Tests structured output with conversation context

### **Key Functions Tested**
1. **`AskStructured[T]`**: Single-question structured output generation
2. **`AskWithHistoryStructured[T]`**: Structured output with conversation history
3. **Schema Validation**: Ensures output matches user-defined JSON schemas
4. **Error Handling**: Graceful handling of LLM failures and validation errors

## 🚀 **Quick Start**

### **Prerequisites**
- Go 1.24.4 or later
- OpenAI API key (for GPT-3.5-turbo)
- AWS credentials (optional, for Bedrock testing)

### **Environment Setup**
```bash
# Required for OpenAI
export OPENAI_API_KEY=your_openai_key_here

# Optional for AWS Bedrock
export AWS_ACCESS_KEY_ID=your_access_key
export AWS_SECRET_ACCESS_KEY=your_secret_key
export AWS_REGION=us-east-1

# Test configuration
export TRACING_PROVIDER=console
export TOOL_OUTPUT_FOLDER=./logs
export TOOL_OUTPUT_THRESHOLD=1000
```

### **Run the Test**
```bash
# Navigate to test directory
cd external_example/structured_output_test

# Run the test
./run_test.sh
```

## 📋 **Test Scenarios**

### **Test 1: TodoList Generation**
- **Prompt**: "Create a simple todo list with 2 tasks for learning Go programming"
- **Schema**: Nested structure with tasks, subtasks, and dependencies
- **Expected Output**: Structured TodoList with learning tasks

### **Test 2: Project Planning**
- **Prompt**: "Create a project plan for developing a new AI-powered chatbot with 3 team members and 4 milestones"
- **Schema**: Project structure with team members and milestone tracking
- **Expected Output**: Structured Project with development plan

### **Test 3: Financial Analysis**
- **Prompt**: "Create a quarterly financial report for a tech startup showing revenue growth, profitability metrics, and key financial ratios"
- **Schema**: Financial data with numeric metrics and ratios
- **Expected Output**: Structured FinancialReport with business metrics

**Note**: The current test implementation focuses on `AskStructured` functionality. `AskWithHistoryStructured` testing will be added in a future update when the external package dependency issues are resolved.

## 🔧 **Technical Details**

### **Structured Output Functions**
```go
// Single question with structured output
func AskStructured[T any](a Agent, ctx context.Context, question string, schema T, schemaString string) (T, error)

// Conversation history with structured output
func AskWithHistoryStructured[T any](a Agent, ctx context.Context, messages []llmtypes.MessageContent, schema T, schemaString string) (T, []llmtypes.MessageContent, error)
```

### **Schema Definition Pattern**
```go
schema := `{
    "type": "object",
    "properties": {
        "title": {"type": "string"},
        "description": {"type": "string"},
        "tasks": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "id": {"type": "string"},
                    "title": {"type": "string"},
                    "status": {"type": "string"}
                },
                "required": ["id", "title", "status"]
            }
        }
    },
    "required": ["title", "description", "tasks"]
}`
```

### **Integration with MCP Agent**
The external agent structured output functions:
1. **Delegate to MCP Agent**: Use the underlying `mcpagent.Agent` for LLM operations
2. **Maintain Compatibility**: Preserve the external agent interface
3. **Handle Context**: Properly manage context cancellation and timeouts
4. **Error Propagation**: Return meaningful errors for debugging

## 📊 **Expected Output Format**

### **TodoList Example**
```json
{
  "title": "Go Programming Learning Plan",
  "description": "A structured approach to learning Go programming language",
  "tasks": [
    {
      "id": "task-001",
      "title": "Set Up Go Development Environment",
      "status": "pending",
      "priority": "high",
      "subtasks": [
        {
          "id": "subtask-001",
          "title": "Install Go",
          "status": "pending"
        }
      ],
      "dependencies": []
    }
  ],
  "status": "active"
}
```

### **Project Example**
```json
{
  "name": "AI-Powered Chatbot Development",
  "description": "Development of an intelligent chatbot using modern AI technologies",
  "team_members": ["Alice", "Bob", "Charlie"],
  "milestones": [
    {
      "id": "milestone-001",
      "title": "Requirements Analysis",
      "description": "Define chatbot requirements and user stories",
      "due_date": "2025-02-15",
      "status": "planned"
    }
  ],
  "status": "planning"
}
```

## 🧪 **Testing and Validation**

### **Schema Validation**
- **JSON Format**: Ensures output is valid JSON
- **Structure Compliance**: Validates against user-defined schemas
- **Required Fields**: Checks that all required fields are present
- **Type Safety**: Ensures data types match schema specifications

### **Error Handling**
- **LLM Failures**: Graceful handling of API errors
- **Validation Errors**: Clear error messages for schema mismatches
- **Context Cancellation**: Proper handling of timeouts and cancellation
- **Type Conversion**: Safe conversion between JSON and Go structs

### **Performance Considerations**
- **Token Usage**: Monitors LLM token consumption
- **Response Time**: Tracks structured output generation time
- **Memory Usage**: Efficient handling of large responses
- **Caching**: Reuses structured output generators when possible

## 🔍 **Debugging and Troubleshooting**

### **Common Issues**
1. **Import Errors**: Ensure `go mod tidy` is run after changes
2. **API Key Issues**: Verify environment variables are set correctly
3. **Schema Mismatches**: Check that JSON schemas are valid
4. **Type Errors**: Ensure Go structs match JSON schemas

### **Debug Mode**
```bash
# Enable debug logging
export TRACING_PROVIDER=console
export LANGFUSE_DEBUG=true

# Run with verbose output
go run main.go
```

### **Log Analysis**
- **Tool Output**: Check `./logs` directory for large responses
- **Tracing**: Console output shows detailed operation flow
- **Error Messages**: Clear error reporting for debugging

## 📚 **Related Documentation**

- **External Agent**: `../../agent_go/pkg/external/README.md`
- **MCP Agent**: `../../agent_go/pkg/mcpagent/README.md`
- **Structured Output**: `../../agent_go/pkg/mcpagent/structured_output.go`
- **Testing Framework**: `../../agent_go/cmd/testing/README.md`

## 🎉 **Success Criteria**

The test is considered successful when:
- ✅ All structured output functions execute without errors
- ✅ Generated JSON matches the defined schemas
- ✅ Conversation history is properly maintained
- ✅ Error handling works correctly for edge cases
- ✅ Performance is within acceptable limits

## 🚀 **Next Steps**

After successful testing:
1. **Integration**: Use structured output in production applications
2. **Schema Library**: Build a collection of common schemas
3. **Validation**: Add more sophisticated schema validation
4. **Performance**: Optimize for high-volume usage
5. **Monitoring**: Add metrics and alerting for production use

---

**Test Version**: 1.0.0  
**Last Updated**: 2025-01-27  
**External Agent Version**: Compatible with latest  
**Go Version**: 1.24.4+
