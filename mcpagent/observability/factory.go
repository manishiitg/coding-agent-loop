package observability

import (
	"strings"

	loggerv2 "mcpagent/logger/v2"
)

const (
	ProviderLangfuse = "langfuse"
	ProviderNoop     = "noop"
)

// GetTracer returns a Tracer implementation based on the provided provider string.
func GetTracer(provider string) Tracer {
	provider = strings.ToLower(provider)

	switch provider {
	case "langfuse":
		if tracer, err := NewLangfuseTracer(); err == nil {
			return tracer
		}
		// Fallback to noop if Langfuse init fails
		return NoopTracer{}
	case "noop":
		return NoopTracer{}
	default:
		return NoopTracer{}
	}
}

// GetTracerWithLogger returns a Tracer implementation based on the provided provider string with an injected logger.
func GetTracerWithLogger(provider string, logger loggerv2.Logger) Tracer {
	provider = strings.ToLower(provider)

	switch provider {
	case "langfuse":
		if tracer, err := NewLangfuseTracerWithLogger(logger); err == nil {
			return tracer
		}
		// Fallback to noop if Langfuse init fails
		return NoopTracer{}
	case "noop":
		return NoopTracer{}
	default:
		return NoopTracer{}
	}
}
