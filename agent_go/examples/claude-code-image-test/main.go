// claude-code-image-test is a minimal example to test if Claude Code CLI can read images.
//
// It reads an image file, base64-encodes it, and sends it to Claude Code CLI
// via the multi-llm-provider adapter with a fixed prompt asking to describe the image.
//
// Usage:
//
//	go run ./examples/claude-code-image-test/ --image /path/to/image.png
//	go run ./examples/claude-code-image-test/ --image /path/to/image.png --prompt "What text do you see?"
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/claudecode"
)

var (
	flagImage  = flag.String("image", "", "path to image file (png, jpg, gif, webp)")
	flagPrompt = flag.String("prompt", "Describe this image in detail. What do you see?", "prompt to send with the image")
)

func main() {
	flag.Parse()

	if *flagImage == "" {
		fmt.Fprintf(os.Stderr, "Usage: go run ./examples/claude-code-image-test/ --image /path/to/image.png\n")
		os.Exit(1)
	}

	// Read and encode the image
	imageData, err := os.ReadFile(*flagImage)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read image: %v\n", err)
		os.Exit(1)
	}

	b64Data := base64.StdEncoding.EncodeToString(imageData)
	mediaType := detectMediaType(*flagImage)

	fmt.Fprintf(os.Stderr, "Image: %s (%s, %d bytes, %d base64 chars)\n", *flagImage, mediaType, len(imageData), len(b64Data))
	fmt.Fprintf(os.Stderr, "Prompt: %s\n", *flagPrompt)
	fmt.Fprintf(os.Stderr, "Sending to Claude Code CLI...\n\n")

	// Create adapter (no API key needed for Claude Code CLI)
	adapter := claudecode.NewClaudeCodeAdapter("", "claude-code", &noopLogger{})

	// Build message with image + text
	messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.ImageContent{
					SourceType: "base64",
					MediaType:  mediaType,
					Data:       b64Data,
				},
				llmtypes.TextContent{
					Text: *flagPrompt,
				},
			},
		},
	}

	// Stream output as it arrives
	streamFn := func(chunk llmtypes.StreamChunk) {
		if chunk.Type == llmtypes.StreamChunkTypeContent {
			fmt.Print(chunk.Content)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := adapter.GenerateContent(ctx, messages, llmtypes.WithStreamingFunc(streamFn))
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
		os.Exit(1)
	}

	// Print final result
	if resp != nil && len(resp.Choices) > 0 {
		fmt.Printf("\n\n--- Final Result ---\n%s\n", resp.Choices[0].Content)
		if resp.Usage != nil {
			fmt.Fprintf(os.Stderr, "\n[usage] %d input / %d output / %d total tokens\n",
				resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens)
		}
	}
}

func detectMediaType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

// noopLogger satisfies the interfaces.Logger interface
type noopLogger struct{}

func (l *noopLogger) Debugf(format string, args ...interface{}) {}
func (l *noopLogger) Infof(format string, args ...interface{})  {}
func (l *noopLogger) Warnf(format string, args ...interface{})  {}
func (l *noopLogger) Errorf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[ERROR] "+format+"\n", args...)
}
