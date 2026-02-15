// Package grpcserver provides a gRPC server implementation for MCPAgent.
//
// This package enables bidirectional streaming communication between
// Node.js clients and the Go agent server, supporting:
//   - Agent lifecycle management (create, get, list, destroy)
//   - Streaming conversations with real-time token delivery
//   - Inline tool callbacks without separate HTTP server
//   - Full observability via event streaming
//
// The gRPC server runs alongside the existing HTTP server on a separate
// Unix socket, allowing gradual migration from HTTP to gRPC.
package grpcserver

//go:generate protoc --proto_path=../proto --go_out=./pb --go_opt=paths=source_relative --go-grpc_out=./pb --go-grpc_opt=paths=source_relative ../proto/agent.proto
