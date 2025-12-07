package testing

import (
	"os"

	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"
)

// InitializeTracer initializes the appropriate tracer based on environment configuration.
// This is a shared utility function used across all test files in cmd/testing/.
//
// Environment Variables:
//   - TRACING_PROVIDER: Set to "langfuse" to enable Langfuse tracing
//   - LANGFUSE_PUBLIC_KEY: Your Langfuse public key
//   - LANGFUSE_SECRET_KEY: Your Langfuse secret key
//   - LANGFUSE_HOST: Langfuse host (optional, defaults to https://cloud.langfuse.com)
//
// Behavior:
//   - If TRACING_PROVIDER=langfuse and credentials are available: Uses Langfuse tracer
//   - If TRACING_PROVIDER=langfuse but credentials missing: Falls back to noop tracer with warning
//   - If TRACING_PROVIDER is not set or any other value: Uses noop tracer
//
// Returns:
//   - observability.Tracer: Either Langfuse tracer or noop tracer
//
// Example Usage:
//
//	tracer := InitializeTracer(logger)
//	// Use tracer for event emission, tracing, etc.
func InitializeTracer(logger loggerv2.Logger) observability.Tracer {
	// Check if Langfuse is enabled via environment
	tracingProvider := os.Getenv("TRACING_PROVIDER")
	if tracingProvider == "langfuse" {
		// Removed verbose logging

		// Check if Langfuse credentials are available
		publicKey := os.Getenv("LANGFUSE_PUBLIC_KEY")
		secretKey := os.Getenv("LANGFUSE_SECRET_KEY")

		if publicKey != "" && secretKey != "" {
			tracer := observability.GetTracerWithLogger("langfuse", logger)
			// Removed verbose logging
			return tracer
		} else {
			// Removed verbose logging
		}
	}

	// Default to noop tracer
	// Removed verbose logging
	return observability.GetTracerWithLogger("noop", logger)
}

// GetTracingInfo returns information about the current tracing configuration
func GetTracingInfo() map[string]interface{} {
	tracingProvider := os.Getenv("TRACING_PROVIDER")
	publicKey := os.Getenv("LANGFUSE_PUBLIC_KEY")
	secretKey := os.Getenv("LANGFUSE_SECRET_KEY")
	host := os.Getenv("LANGFUSE_HOST")

	if host == "" {
		host = "https://cloud.langfuse.com"
	}

	return map[string]interface{}{
		"tracing_provider": tracingProvider,
		"langfuse_enabled": tracingProvider == "langfuse" && publicKey != "" && secretKey != "",
		"langfuse_host":    host,
		"has_credentials":  publicKey != "" && secretKey != "",
	}
}

// Removed adapter - logger is already loggerv2.Logger
