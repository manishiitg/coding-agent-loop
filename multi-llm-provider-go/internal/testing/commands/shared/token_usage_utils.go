package shared

import (
	"context"
	"fmt"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// ValidateReasoningTokensInUsage validates that ReasoningTokens are present in the unified Usage field
func ValidateReasoningTokensInUsage(usage *llmtypes.Usage, expectedModel string) bool {
	if usage == nil {
		fmt.Printf("❌ VALIDATION FAILED: Usage field is nil\n")
		return false
	}

	if usage.ReasoningTokens == nil {
		fmt.Printf("❌ VALIDATION FAILED: ReasoningTokens not found in unified Usage field\n")
		fmt.Printf("   Expected ReasoningTokens to be present for %s with reasoning_effort=high\n", expectedModel)
		return false
	}

	fmt.Printf("✅ VALIDATION PASSED: ReasoningTokens found in unified Usage field: %d\n", *usage.ReasoningTokens)
	return true
}

// ValidateThoughtsTokensInUsage validates that ThoughtsTokens are present in the unified Usage field
func ValidateThoughtsTokensInUsage(usage *llmtypes.Usage, expectedModel string) bool {
	if usage == nil {
		fmt.Printf("❌ VALIDATION FAILED: Usage field is nil\n")
		return false
	}

	if usage.ThoughtsTokens == nil {
		fmt.Printf("❌ VALIDATION FAILED: ThoughtsTokens not found in unified Usage field\n")
		fmt.Printf("   Expected ThoughtsTokens to be present for %s with thinking_level=high\n", expectedModel)
		return false
	}

	fmt.Printf("✅ VALIDATION PASSED: ThoughtsTokens found in unified Usage field: %d\n", *usage.ThoughtsTokens)
	return true
}

// TestLLMTokenUsage tests basic token usage extraction from an LLM response
func TestLLMTokenUsage(ctx context.Context, llm llmtypes.Model, messages []llmtypes.MessageContent, prompt string, options ...llmtypes.CallOption) {
	startTime := time.Now()

	fmt.Printf("⏱️  Starting LLM call...\n")
	fmt.Printf("📝 Sending message: %s\n", prompt)

	// Make the LLM call with options
	resp, err := llm.GenerateContent(ctx, messages, options...)

	duration := time.Since(startTime)

	fmt.Printf("\n📊 Token Usage Test Results:\n")
	fmt.Printf("============================\n")

	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		return
	}

	if resp == nil || resp.Choices == nil || len(resp.Choices) == 0 {
		fmt.Printf("❌ No response received\n")
		return
	}

	choice := resp.Choices[0]
	content := choice.Content

	fmt.Printf("✅ Response received successfully!\n")
	fmt.Printf("   Duration: %v\n", duration)
	fmt.Printf("   Response length: %d chars\n", len(content))
	fmt.Printf("   Content: %s\n\n", content)

	// Check for token usage information (using unified Usage field)
	fmt.Printf("🔍 Token Usage Analysis:\n")
	fmt.Printf("========================\n")

	// First check the unified Usage field
	if resp.Usage != nil {
		fmt.Printf("✅ Unified Usage field found!\n")
		fmt.Printf("   Input tokens:  %d\n", resp.Usage.InputTokens)
		fmt.Printf("   Output tokens: %d\n", resp.Usage.OutputTokens)
		fmt.Printf("   Total tokens:  %d\n", resp.Usage.TotalTokens)

		// Validate ReasoningTokens in unified Usage field (if present)
		if resp.Usage.ReasoningTokens != nil {
			fmt.Printf("   ✅ Reasoning tokens in Usage field: %d (OpenAI gpt-5.1, etc.)\n", *resp.Usage.ReasoningTokens)
			fmt.Printf("   ✅ VALIDATION: ReasoningTokens successfully extracted to unified Usage field\n")
		}

		// Validate ThoughtsTokens in unified Usage field (if present)
		if resp.Usage.ThoughtsTokens != nil {
			fmt.Printf("   ✅ Thoughts tokens in Usage field: %d (Gemini 3 Pro, etc.)\n", *resp.Usage.ThoughtsTokens)
			fmt.Printf("   ✅ VALIDATION: ThoughtsTokens successfully extracted to unified Usage field\n")
		}

		fmt.Printf("\n✅ Token usage data is available via unified interface!\n")
		fmt.Printf("   This means proper cost tracking and observability will work\n")
	} else if choice.GenerationInfo != nil {
		fmt.Printf("⚠️  Unified Usage field not found, but GenerationInfo is available\n")
		fmt.Printf("   Falling back to GenerationInfo extraction...\n\n")
	} else {
		fmt.Printf("❌ No token usage found in response (neither Usage nor GenerationInfo)\n")
		fmt.Printf("   This means the LLM provider is not providing token usage data\n")
		fmt.Printf("   Token usage will need to be estimated\n")
		return
	}

	// Still check GenerationInfo for advanced metadata (cache tokens, reasoning tokens, etc.)
	var foundTokens bool
	var info *llmtypes.GenerationInfo
	if choice.GenerationInfo != nil {
		fmt.Printf("\n🔍 Checking GenerationInfo for advanced metadata...\n\n")

		// Check for specific token fields
		tokenFields := map[string]string{
			"input_tokens":      "Input tokens",
			"output_tokens":     "Output tokens",
			"total_tokens":      "Total tokens",
			"prompt_tokens":     "Prompt tokens",
			"completion_tokens": "Completion tokens",
			// OpenAI-specific field names
			"PromptTokens":     "Prompt tokens (OpenAI)",
			"CompletionTokens": "Completion tokens (OpenAI)",
			"TotalTokens":      "Total tokens (OpenAI)",
			"ReasoningTokens":  "Reasoning tokens (OpenAI o3)",
			// Anthropic-specific field names
			"InputTokens":  "Input tokens (Anthropic)",
			"OutputTokens": "Output tokens (Anthropic)",
			// OpenRouter cache token fields
			"cache_tokens":     "Cache tokens (OpenRouter)",
			"cache_discount":   "Cache discount (OpenRouter)",
			"cache_write_cost": "Cache write cost (OpenRouter)",
			"cache_read_cost":  "Cache read cost (OpenRouter)",
		}

		foundTokens = false
		info = choice.GenerationInfo
		if info != nil {
			// Check typed fields
			if info.InputTokens != nil {
				fmt.Printf("✅ %s: %v\n", tokenFields["input_tokens"], *info.InputTokens)
				foundTokens = true
			}
			if info.OutputTokens != nil {
				fmt.Printf("✅ %s: %v\n", tokenFields["output_tokens"], *info.OutputTokens)
				foundTokens = true
			}
			if info.TotalTokens != nil {
				fmt.Printf("✅ %s: %v\n", tokenFields["total_tokens"], *info.TotalTokens)
				foundTokens = true
			}
			// Check for reasoning tokens (typed field - for o3 models)
			if info.ReasoningTokens != nil {
				fmt.Printf("✅ %s: %d (from typed field)\n", tokenFields["ReasoningTokens"], *info.ReasoningTokens)
				foundTokens = true
			}
			// Check for cached tokens
			if info.CachedContentTokens != nil {
				fmt.Printf("✅ Cached Content Tokens: %d\n", *info.CachedContentTokens)
				foundTokens = true
			}
			if info.CacheDiscount != nil {
				fmt.Printf("✅ Cache Discount: %.4f (%.2f%%)\n", *info.CacheDiscount, *info.CacheDiscount*100)
				foundTokens = true
			}
			// Check Additional map for other fields
			if info.Additional != nil {
				for field, label := range tokenFields {
					if field != "input_tokens" && field != "output_tokens" && field != "total_tokens" && field != "ReasoningTokens" {
						if value, ok := info.Additional[field]; ok {
							fmt.Printf("✅ %s: %v\n", label, value)
							foundTokens = true
						}
					}
				}
				// Check for reasoning tokens in Additional map (fallback)
				if info.ReasoningTokens == nil {
					if value, ok := info.Additional["ReasoningTokens"]; ok {
						fmt.Printf("✅ %s: %v (from Additional map)\n", tokenFields["ReasoningTokens"], value)
						foundTokens = true
					}
				}
				// Check for cache-related fields in Additional
				cacheFields := []string{"cache_tokens", "cache_read_tokens", "cache_write_tokens", "CacheReadInputTokens", "CacheCreationInputTokens"}
				for _, field := range cacheFields {
					if value, ok := info.Additional[field]; ok {
						fmt.Printf("✅ Cache field (%s): %v\n", field, value)
						foundTokens = true
					}
				}
			}
		}
	}

	// Summary - already printed Usage if available above
	if resp.Usage == nil && !foundTokens {
		fmt.Printf("❌ No standard token fields found\n")
		fmt.Printf("   GenerationInfo: %+v\n", info)
		fmt.Printf("\n   This suggests the LLM provider doesn't return token usage\n")
	}

	// Show all available GenerationInfo for debugging
	fmt.Printf("\n🔍 Complete GenerationInfo:\n")
	fmt.Printf("==========================\n")
	if info != nil {
		fmt.Printf("   InputTokens: %v\n", info.InputTokens)
		fmt.Printf("   OutputTokens: %v\n", info.OutputTokens)
		fmt.Printf("   TotalTokens: %v\n", info.TotalTokens)
		fmt.Printf("   ReasoningTokens: %v\n", info.ReasoningTokens)
		fmt.Printf("   CachedContentTokens: %v\n", info.CachedContentTokens)
		fmt.Printf("   CacheDiscount: %v\n", info.CacheDiscount)
		if info.Additional != nil {
			for key, value := range info.Additional {
				fmt.Printf("   %s: %v (type: %T)\n", key, value, value)
			}
		}
	} else {
		fmt.Printf("   GenerationInfo is nil\n")
	}

	// Show raw response structure for debugging
	fmt.Printf("\n🔍 Raw Response Structure:\n")
	fmt.Printf("==========================\n")
	fmt.Printf("   Response type: %T\n", resp)
	fmt.Printf("   Choices count: %d\n", len(resp.Choices))
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		fmt.Printf("   Choice type: %T\n", choice)
		fmt.Printf("   Content type: %T\n", choice.Content)
		fmt.Printf("   GenerationInfo type: %T\n", choice.GenerationInfo)
		if choice.GenerationInfo != nil {
			info := choice.GenerationInfo
			fmt.Printf("   GenerationInfo: InputTokens=%v, OutputTokens=%v, TotalTokens=%v\n",
				info.InputTokens, info.OutputTokens, info.TotalTokens)
		}
	}
}

// TestLLMTokenUsageWithTools tests token usage extraction when using tools
func TestLLMTokenUsageWithTools(ctx context.Context, llm llmtypes.Model, messages []llmtypes.MessageContent, tools []llmtypes.Tool) {
	startTime := time.Now()

	fmt.Printf("⏱️  Starting LLM call with tools...\n")
	fmt.Printf("📝 Sending message: %s\n", ExtractMessageText(messages))
	fmt.Printf("🔧 Tools count: %d\n", len(tools))

	// Make the LLM call with tools
	resp, err := llm.GenerateContent(ctx, messages, llmtypes.WithTools(tools))

	duration := time.Since(startTime)

	fmt.Printf("\n📊 Token Usage Test Results (with tools):\n")
	fmt.Printf("==========================================\n")

	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		return
	}

	if resp == nil || resp.Choices == nil || len(resp.Choices) == 0 {
		fmt.Printf("❌ No response received\n")
		return
	}

	choice := resp.Choices[0]
	content := choice.Content
	hasToolCalls := len(choice.ToolCalls) > 0

	fmt.Printf("✅ Response received successfully!\n")
	fmt.Printf("   Duration: %v\n", duration)
	if hasToolCalls {
		fmt.Printf("   Tool calls: %d\n", len(choice.ToolCalls))
		for i, tc := range choice.ToolCalls {
			fmt.Printf("      Tool %d: %s\n", i+1, tc.FunctionCall.Name)
		}
	} else {
		fmt.Printf("   Response length: %d chars\n", len(content))
		if len(content) > 0 {
			preview := content
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			fmt.Printf("   Content: %s\n", preview)
		}
	}
	fmt.Printf("\n")

	// Check for token usage information (using unified Usage field)
	fmt.Printf("🔍 Token Usage Analysis (with tools):\n")
	fmt.Printf("======================================\n")

	// First check the unified Usage field
	if resp.Usage != nil {
		fmt.Printf("✅ Unified Usage field found!\n")
		fmt.Printf("   Input tokens:  %d\n", resp.Usage.InputTokens)
		fmt.Printf("   Output tokens: %d\n", resp.Usage.OutputTokens)
		fmt.Printf("   Total tokens:  %d\n", resp.Usage.TotalTokens)
		// Validate ReasoningTokens in unified Usage field (if present)
		if resp.Usage.ReasoningTokens != nil {
			fmt.Printf("   ✅ Reasoning tokens in Usage field: %d (OpenAI gpt-5.1, etc.)\n", *resp.Usage.ReasoningTokens)
			fmt.Printf("   ✅ VALIDATION: ReasoningTokens successfully extracted to unified Usage field\n")
		}

		// Validate ThoughtsTokens in unified Usage field (if present)
		if resp.Usage.ThoughtsTokens != nil {
			fmt.Printf("   ✅ Thoughts tokens in Usage field: %d (Gemini 3 Pro, etc.)\n", *resp.Usage.ThoughtsTokens)
			fmt.Printf("   ✅ VALIDATION: ThoughtsTokens successfully extracted to unified Usage field\n")
		}

		fmt.Printf("\n✅ Token usage data extracted successfully!\n")
	} else if choice.GenerationInfo != nil {
		fmt.Printf("⚠️  Unified Usage field not found, but GenerationInfo is available\n")
		fmt.Printf("   Falling back to GenerationInfo extraction...\n\n")
	} else {
		fmt.Printf("❌ No token usage found in response\n")
		fmt.Printf("   Token usage extraction failed\n")
		return
	}

	// Still check GenerationInfo for advanced metadata
	var foundTokens bool
	var info *llmtypes.GenerationInfo
	if choice.GenerationInfo != nil {
		fmt.Printf("\n🔍 Checking GenerationInfo for advanced metadata...\n\n")

		// Check for specific token fields (Google GenAI uses these field names)
		tokenFields := map[string]string{
			"input_tokens":  "Input tokens",
			"output_tokens": "Output tokens",
			"total_tokens":  "Total tokens",
		}

		foundTokens = false
		var inputTokens, outputTokens, totalTokens interface{}
		info = choice.GenerationInfo

		if info != nil {
			// Check typed fields
			if info.InputTokens != nil {
				inputTokens = *info.InputTokens
				fmt.Printf("✅ %s: %v\n", tokenFields["input_tokens"], inputTokens)
				foundTokens = true
			}
			if info.OutputTokens != nil {
				outputTokens = *info.OutputTokens
				fmt.Printf("✅ %s: %v\n", tokenFields["output_tokens"], outputTokens)
				foundTokens = true
			}
			if info.TotalTokens != nil {
				totalTokens = *info.TotalTokens
				fmt.Printf("✅ %s: %v\n", tokenFields["total_tokens"], totalTokens)
				foundTokens = true
			}
			// Check Additional map for other fields
			if info.Additional != nil {
				for field, label := range tokenFields {
					if field != "input_tokens" && field != "output_tokens" && field != "total_tokens" {
						if value, ok := info.Additional[field]; ok {
							fmt.Printf("✅ %s: %v\n", label, value)
							foundTokens = true
						}
					}
				}
			}
		}
	}

	// Validate token counts using unified Usage field if available
	if resp.Usage != nil {
		fmt.Printf("\n🔍 Token Usage Validation (from unified Usage field):\n")
		fmt.Printf("   Input tokens:  %d\n", resp.Usage.InputTokens)
		fmt.Printf("   Output tokens: %d\n", resp.Usage.OutputTokens)
		fmt.Printf("   Total tokens:  %d\n", resp.Usage.TotalTokens)

		// Check if total matches sum (allowing for slight discrepancies)
		calculatedTotal := resp.Usage.InputTokens + resp.Usage.OutputTokens
		if resp.Usage.TotalTokens > 0 {
			diff := resp.Usage.TotalTokens - calculatedTotal
			if diff < 0 {
				diff = -diff
			}
			if resp.Usage.TotalTokens == calculatedTotal {
				fmt.Printf("   ✅ Total tokens matches input + output\n")
			} else if diff <= 2 {
				fmt.Printf("   ⚠️  Total tokens differs from input+output by %d (acceptable)\n", diff)
			} else {
				fmt.Printf("   ⚠️  Total tokens (%d) differs significantly from input+output (%d)\n", resp.Usage.TotalTokens, calculatedTotal)
			}
		}

		// Check for reasonable token counts
		if resp.Usage.InputTokens > 0 && resp.Usage.OutputTokens >= 0 {
			fmt.Printf("   ✅ Token counts are reasonable\n")
		} else {
			fmt.Printf("   ⚠️  Unusual token counts detected\n")
		}
	} else if !foundTokens {
		fmt.Printf("❌ No standard token fields found in GenerationInfo\n")
		fmt.Printf("   GenerationInfo: %+v\n", info)
		fmt.Printf("\n   This suggests the adapter is not extracting token usage correctly\n")
	}

	// Show all available GenerationInfo for debugging
	fmt.Printf("\n🔍 Complete GenerationInfo:\n")
	fmt.Printf("==========================\n")
	if info != nil {
		fmt.Printf("   InputTokens: %v\n", info.InputTokens)
		fmt.Printf("   OutputTokens: %v\n", info.OutputTokens)
		fmt.Printf("   TotalTokens: %v\n", info.TotalTokens)
		fmt.Printf("   ReasoningTokens: %v\n", info.ReasoningTokens)
		fmt.Printf("   CachedContentTokens: %v\n", info.CachedContentTokens)
		fmt.Printf("   CacheDiscount: %v\n", info.CacheDiscount)
		if info.Additional != nil {
			for key, value := range info.Additional {
				fmt.Printf("   %s: %v (type: %T)\n", key, value, value)
			}
		}
	} else {
		fmt.Printf("   GenerationInfo is nil\n")
	}
}

// TestLLMTokenUsageWithCache tests token usage with multi-turn conversation to verify cache token extraction
func TestLLMTokenUsageWithCache(ctx context.Context, llm llmtypes.Model) {

	// Create a large context document that will be cached
	largeContext := GetLargeContextForCache()

	fmt.Printf("📚 Creating multi-turn conversation with large context for cache testing...\n")
	fmt.Printf("   Context length: %d characters\n", len(largeContext))

	// First turn: Send the large context with an initial question
	fmt.Printf("\n🔄 Turn 1: Initial request with large context\n")
	fmt.Printf("============================================\n")
	fmt.Printf("   This request will create the cache for the large context\n")

	turn1Context := largeContext + "\n\nBased on this guide, what are the key principles for code quality?"
	turn1Messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: turn1Context},
			},
		},
	}

	startTime := time.Now()
	resp1, err := llm.GenerateContent(ctx, turn1Messages)
	duration1 := time.Since(startTime)

	if err != nil {
		fmt.Printf("❌ Turn 1 Error: %v\n", err)
		return
	}

	if resp1 == nil || resp1.Choices == nil || len(resp1.Choices) == 0 {
		fmt.Printf("❌ Turn 1: No response received\n")
		return
	}

	choice1 := resp1.Choices[0]
	fmt.Printf("✅ Turn 1 completed in %v\n", duration1)

	// Display basic token usage from unified Usage field
	if resp1.Usage != nil {
		fmt.Printf("   Turn 1 Tokens (from unified Usage) - Input: %d, Output: %d, Total: %d\n",
			resp1.Usage.InputTokens, resp1.Usage.OutputTokens, resp1.Usage.TotalTokens)
	}

	if choice1 != nil {
		AnalyzeTurn1CacheInfo(choice1)
	}

	// Second turn: Send the EXACT same message structure as Turn 1 to trigger caching
	fmt.Printf("\n🔄 Turn 2: Follow-up request (should use cached context)\n")
	fmt.Printf("======================================================\n")
	fmt.Printf("   Sending EXACT same message structure as Turn 1 to trigger caching...\n")
	fmt.Printf("   Waiting 2 seconds to ensure cache is ready...\n")
	time.Sleep(2 * time.Second)

	turn2Context := largeContext + "\n\nBased on this guide, what are the key principles for code quality?"
	turn2Messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: turn2Context},
			},
		},
	}

	startTime = time.Now()
	resp2, err := llm.GenerateContent(ctx, turn2Messages)
	duration2 := time.Since(startTime)

	if err != nil {
		fmt.Printf("❌ Turn 2 Error: %v\n", err)
		return
	}

	if resp2 == nil || resp2.Choices == nil || len(resp2.Choices) == 0 {
		fmt.Printf("❌ Turn 2: No response received\n")
		return
	}

	choice2 := resp2.Choices[0]
	fmt.Printf("✅ Turn 2 completed in %v\n", duration2)

	// Display basic token usage from unified Usage field
	if resp2.Usage != nil {
		fmt.Printf("   Turn 2 Tokens (from unified Usage) - Input: %d, Output: %d, Total: %d\n",
			resp2.Usage.InputTokens, resp2.Usage.OutputTokens, resp2.Usage.TotalTokens)
	}

	// Analyze token usage and cache information
	if choice1 != nil && choice2 != nil {
		AnalyzeCacheTokenUsage(choice1, choice2, resp1.Usage, resp2.Usage)
	}
}

// ExtractMessageText extracts text from messages for logging
func ExtractMessageText(messages []llmtypes.MessageContent) string {
	if len(messages) == 0 {
		return ""
	}
	firstMsg := messages[0]
	for _, part := range firstMsg.Parts {
		if textPart, ok := part.(llmtypes.TextContent); ok {
			text := textPart.Text
			if len(text) > 100 {
				return text[:100] + "..."
			}
			return text
		}
	}
	return ""
}

// ExtractIntValue safely extracts an integer value from interface{}
func ExtractIntValue(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int32:
		return int(val)
	case int64:
		return int(val)
	case float32:
		return int(val)
	case float64:
		return int(val)
	default:
		return 0
	}
}

// GetLargeContextForCache returns a large context string for cache testing
func GetLargeContextForCache() string {
	return `The following is a comprehensive guide to software engineering best practices and methodologies:

1. **Version Control Systems**: Always use version control systems like Git for all projects. Commit frequently with meaningful, descriptive commit messages that explain what changed and why. Use branches for features, bug fixes, and experiments. Never commit directly to main/master branch. Use pull requests or merge requests for code review before merging. Tag releases appropriately. Keep commit history clean and organized. Use .gitignore to exclude unnecessary files. Understand branching strategies like Git Flow or GitHub Flow.

2. **Code Review Process**: All code changes should be reviewed by at least one other developer before merging. Code reviews help catch bugs early, improve code quality, share knowledge across the team, and ensure consistency. Reviewers should check for correctness, performance, security, maintainability, and adherence to coding standards. Provide constructive feedback. Respond to review comments promptly. Use automated tools to catch common issues before human review.

3. **Testing Strategies**: Write comprehensive tests for your code. Aim for high test coverage, especially for critical business logic. Use unit tests for individual functions and methods. Use integration tests for component interactions. Use end-to-end tests for complete workflows. Write tests before or alongside code (TDD/BDD). Keep tests fast, independent, and maintainable. Use mocking and stubbing appropriately. Test edge cases and error conditions. Automate test execution in CI/CD pipelines.

4. **Documentation Standards**: Document your code, APIs, architecture decisions, and processes. Good documentation helps new team members onboard quickly and serves as a reference for future work. Write clear comments explaining why, not what. Keep README files up to date. Document API endpoints with examples. Maintain architecture decision records (ADRs). Keep documentation close to code when possible. Use tools like JSDoc, GoDoc, or Sphinx for API documentation.

5. **Error Handling Patterns**: Always handle errors properly and explicitly. Don't ignore errors or use empty catch blocks. Provide meaningful error messages that help with debugging. Log errors with appropriate context. Use structured error types. Return errors early (fail fast). Handle errors at the appropriate level. Don't expose internal errors to users. Use error wrapping to preserve error context. Implement retry logic for transient failures.

6. **Security Best Practices**: Follow security best practices throughout development. Validate and sanitize all user inputs. Use parameterized queries to prevent SQL injection. Keep dependencies updated to patch security vulnerabilities. Use HTTPS for all network communication. Implement proper authentication and authorization. Follow principle of least privilege. Encrypt sensitive data at rest and in transit. Regular security audits and penetration testing. Stay informed about security advisories.

7. **Performance Optimization**: Write efficient code, but don't optimize prematurely. Profile your code to identify actual bottlenecks before optimizing. Use caching appropriately to improve performance. Optimize database queries. Use connection pooling. Implement pagination for large datasets. Use CDNs for static assets. Minimize network round trips. Consider async processing for long-running tasks. Monitor performance metrics in production.

8. **Code Quality Standards**: Follow coding standards and style guides consistently. Use linters and formatters to maintain consistency automatically. Refactor code regularly to keep it maintainable. Remove dead code. Keep functions small and focused (single responsibility). Use meaningful variable and function names. Avoid deep nesting. Keep cyclomatic complexity low. Use design patterns appropriately. Write self-documenting code.

9. **Continuous Integration and Deployment**: Automate your build, test, and deployment processes completely. Use CI/CD pipelines to catch issues early and deploy frequently. Run tests automatically on every commit. Use feature flags for gradual rollouts. Implement blue-green or canary deployments. Monitor deployments closely. Have rollback procedures ready. Automate infrastructure provisioning. Use infrastructure as code (IaC).

10. **Monitoring and Logging**: Implement comprehensive logging and monitoring systems. Log important events, errors, and state changes with appropriate log levels. Use structured logging with consistent formats. Monitor application performance metrics, error rates, and business metrics. Set up alerts for critical issues. Use distributed tracing for microservices. Keep logs searchable and analyzable. Implement log rotation and retention policies. Use monitoring tools like Prometheus, Grafana, or Datadog.

This comprehensive guide should be followed by all software engineers to ensure high-quality, maintainable, secure, and performant software systems. These practices form the foundation of professional software development and are essential for building reliable applications that can scale and evolve over time.`
}

// AnalyzeTurn1CacheInfo analyzes cache information from Turn 1 response
func AnalyzeTurn1CacheInfo(choice *llmtypes.ContentChoice) {
	if choice.GenerationInfo != nil {
		input1 := 0
		output1 := 0
		if choice.GenerationInfo.InputTokens != nil {
			input1 = *choice.GenerationInfo.InputTokens
		}
		if choice.GenerationInfo.OutputTokens != nil {
			output1 = *choice.GenerationInfo.OutputTokens
		}
		fmt.Printf("   Turn 1 Tokens - Input: %d, Output: %d\n", input1, output1)

		// Check for cache creation tokens in Turn 1
		if choice.GenerationInfo.Additional != nil {
			if rawRead, ok := choice.GenerationInfo.Additional["_debug_cache_read_raw"]; ok {
				fmt.Printf("   🔍 Turn 1 Raw CacheReadInputTokens: %v\n", rawRead)
			}
			if rawCreate, ok := choice.GenerationInfo.Additional["_debug_cache_creation_raw"]; ok {
				fmt.Printf("   🔍 Turn 1 Raw CacheCreationInputTokens: %v\n", rawCreate)
			}

			if cacheCreate, ok := choice.GenerationInfo.Additional["CacheCreationInputTokens"]; ok {
				fmt.Printf("   ✅ Turn 1 Cache Creation Tokens: %v (cache was created!)\n", cacheCreate)
			} else {
				fmt.Printf("   ⚠️  Turn 1: No cache creation tokens found (raw value was 0)\n")
			}
		} else {
			fmt.Printf("   ⚠️  Turn 1: Additional map is nil\n")
		}
		if choice.GenerationInfo.CachedContentTokens != nil {
			fmt.Printf("   ✅ Turn 1 CachedContentTokens: %d\n", *choice.GenerationInfo.CachedContentTokens)
		}
	} else {
		fmt.Printf("   ⚠️  Turn 1: GenerationInfo is nil\n")
	}
}

// AnalyzeCacheTokenUsage analyzes cache token usage from Turn 2 response and compares with Turn 1
func AnalyzeCacheTokenUsage(choice1, choice2 *llmtypes.ContentChoice, usage1, usage2 *llmtypes.Usage) {
	fmt.Printf("\n📊 Cache Token Analysis:\n")
	fmt.Printf("========================\n")

	// First, show Turn 2 basic token info (prefer unified Usage field)
	if usage2 != nil {
		fmt.Printf("   Turn 2 Tokens (from unified Usage) - Input: %d, Output: %d, Total: %d\n",
			usage2.InputTokens, usage2.OutputTokens, usage2.TotalTokens)
	} else if choice2.GenerationInfo != nil {
		input2 := 0
		output2 := 0
		if choice2.GenerationInfo.InputTokens != nil {
			input2 = *choice2.GenerationInfo.InputTokens
		}
		if choice2.GenerationInfo.OutputTokens != nil {
			output2 = *choice2.GenerationInfo.OutputTokens
		}
		fmt.Printf("   Turn 2 Tokens - Input: %d, Output: %d\n", input2, output2)
	}

	if choice2.GenerationInfo == nil {
		fmt.Printf("❌ No GenerationInfo found in Turn 2 response\n")
		return
	}

	info := choice2.GenerationInfo
	foundCacheInfo := false

	// Debug: Print raw Usage values if available
	fmt.Printf("\n🔍 Debug: Checking raw response structure...\n")
	if info.Additional != nil {
		if rawRead, ok := info.Additional["_debug_cache_read_raw"]; ok {
			fmt.Printf("   Raw CacheReadInputTokens from API: %v\n", rawRead)
		}
		if rawCreate, ok := info.Additional["_debug_cache_creation_raw"]; ok {
			fmt.Printf("   Raw CacheCreationInputTokens from API: %v\n", rawCreate)
		}
	}

	// Check for cached content tokens (primary field)
	if info.CachedContentTokens != nil {
		cachedTokens := *info.CachedContentTokens
		fmt.Printf("✅ Cached Content Tokens: %d\n", cachedTokens)
		foundCacheInfo = true

		// Calculate cache percentage if we have input tokens
		if info.InputTokens != nil {
			inputTokens := *info.InputTokens
			if inputTokens > 0 {
				cachePercentage := float64(cachedTokens) / float64(inputTokens) * 100
				fmt.Printf("   Cache Hit Rate: %.2f%% (%d of %d input tokens were cached)\n", cachePercentage, cachedTokens, inputTokens)
				nonCachedTokens := inputTokens - cachedTokens
				fmt.Printf("   Non-cached tokens: %d\n", nonCachedTokens)
			}
		}
	} else {
		fmt.Printf("⚠️  CachedContentTokens field is nil\n")
	}

	// Check for cache discount
	if info.CacheDiscount != nil {
		discount := *info.CacheDiscount
		fmt.Printf("✅ Cache Discount: %.4f (%.2f%%)\n", discount, discount*100)
		foundCacheInfo = true
	}

	// Check Additional map for cache-related fields
	if info.Additional != nil {
		fmt.Printf("   Checking Additional map for cache fields...\n")
		cacheFields := map[string]string{
			"cache_tokens":                "Cache tokens",
			"cache_read_tokens":           "Cache read tokens",
			"cache_write_tokens":          "Cache write tokens",
			"cache_discount":              "Cache discount",
			"cache_read_cost":             "Cache read cost",
			"cache_write_cost":            "Cache write cost",
			"CacheReadInputTokens":        "Cache read input tokens (Anthropic)",
			"CacheCreationInputTokens":    "Cache creation input tokens (Anthropic)",
			"cache_read_input_tokens":     "Cache read input tokens (lowercase)",
			"cache_creation_input_tokens": "Cache creation input tokens (lowercase)",
		}

		foundInAdditional := false
		for field, label := range cacheFields {
			if value, ok := info.Additional[field]; ok {
				fmt.Printf("✅ %s: %v\n", label, value)
				foundCacheInfo = true
				foundInAdditional = true
			}
		}
		if !foundInAdditional {
			fmt.Printf("   ⚠️  No cache-related fields found in Additional map\n")
		}
	} else {
		fmt.Printf("⚠️  Additional map is nil\n")
	}

	// Display full token breakdown
	fmt.Printf("\n📊 Full Token Breakdown (Turn 2):\n")
	fmt.Printf("=================================\n")
	if info.InputTokens != nil {
		fmt.Printf("   Input tokens: %d\n", *info.InputTokens)
	}
	if info.OutputTokens != nil {
		fmt.Printf("   Output tokens: %d\n", *info.OutputTokens)
	}
	if info.TotalTokens != nil {
		fmt.Printf("   Total tokens: %d\n", *info.TotalTokens)
	}

	// Compare Turn 1 vs Turn 2 (prefer unified Usage fields)
	if (usage1 != nil && usage2 != nil) || (choice1.GenerationInfo != nil && choice2.GenerationInfo != nil) {
		fmt.Printf("\n📊 Comparison: Turn 1 vs Turn 2\n")
		fmt.Printf("===============================\n")

		var input1, input2 int
		if usage1 != nil && usage2 != nil {
			input1 = usage1.InputTokens
			input2 = usage2.InputTokens
		} else if choice1.GenerationInfo != nil && choice2.GenerationInfo != nil {
			if choice1.GenerationInfo.InputTokens != nil {
				input1 = *choice1.GenerationInfo.InputTokens
			}
			if choice2.GenerationInfo.InputTokens != nil {
				input2 = *choice2.GenerationInfo.InputTokens
			}
		}

		if input1 > 0 && input2 > 0 {
			savings := input1 - input2
			if savings > 0 {
				savingsPercent := float64(savings) / float64(input1) * 100
				fmt.Printf("   Turn 1 Input: %d tokens\n", input1)
				fmt.Printf("   Turn 2 Input: %d tokens\n", input2)
				fmt.Printf("   💰 Token Savings: %d tokens (%.2f%% reduction)\n", savings, savingsPercent)
			} else if foundCacheInfo {
				fmt.Printf("   Turn 1 Input: %d tokens\n", input1)
				fmt.Printf("   Turn 2 Input: %d tokens\n", input2)
				fmt.Printf("   ℹ️  Cache detected but input tokens similar (may include cached tokens in count)\n")
			}
		}
	}

	if !foundCacheInfo {
		fmt.Printf("⚠️  No cache token information found in GenerationInfo\n")
		fmt.Printf("\n   Provider-specific caching requirements:\n")
		fmt.Printf("   - Anthropic: Requires explicit Cache API usage\n")
		fmt.Printf("   - Vertex AI (Gemini): Automatic caching when context is repeated\n")
		fmt.Printf("   - OpenRouter: Requires EXACT prefix matching for cache hits\n")
	} else {
		fmt.Printf("\n✅ Cache token information successfully extracted!\n")
		fmt.Printf("   The test is working correctly - cached tokens detected!\n")
	}

	// Show complete GenerationInfo for debugging
	fmt.Printf("\n🔍 Complete GenerationInfo (Turn 2):\n")
	fmt.Printf("===================================\n")
	if info != nil {
		fmt.Printf("   InputTokens: %v\n", info.InputTokens)
		fmt.Printf("   OutputTokens: %v\n", info.OutputTokens)
		fmt.Printf("   TotalTokens: %v\n", info.TotalTokens)
		fmt.Printf("   ReasoningTokens: %v\n", info.ReasoningTokens)
		fmt.Printf("   CachedContentTokens: %v\n", info.CachedContentTokens)
		fmt.Printf("   CacheDiscount: %v\n", info.CacheDiscount)
		if info.Additional != nil {
			for key, value := range info.Additional {
				fmt.Printf("   %s: %v (type: %T)\n", key, value, value)
			}
		}
	}
}
