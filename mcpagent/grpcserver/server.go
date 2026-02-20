package grpcserver

import (
	"context"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/manishiitg/mcpagent/grpcserver/pb"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// Server represents the gRPC server for MCPAgent
type Server struct {
	grpcServer *grpc.Server
	listener   net.Listener
	socketPath string
	manager    *AgentManager
	service    *AgentService
	logger     loggerv2.Logger
}

// Config holds gRPC server configuration
type Config struct {
	SocketPath        string
	DefaultConfigPath string
	Logger            loggerv2.Logger
	// Optional: share an existing AgentManager
	Manager *AgentManager
}

// NewServer creates a new gRPC server
func NewServer(cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = loggerv2.NewDefault()
	}

	// Use existing manager or create new one
	manager := cfg.Manager
	if manager == nil {
		manager = NewAgentManager(logger, cfg.DefaultConfigPath)
	}

	// Create gRPC server with keepalive settings
	grpcServer := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     5 * time.Minute,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 5 * time.Second,
			Time:                  1 * time.Minute,
			Timeout:               20 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             30 * time.Second,
			PermitWithoutStream: true,
		}),
		// Allow large messages for tool outputs
		grpc.MaxRecvMsgSize(100*1024*1024), // 100MB
		grpc.MaxSendMsgSize(100*1024*1024), // 100MB
	)

	// Create and register the service
	service := NewAgentService(manager, logger)
	pb.RegisterAgentServiceServer(grpcServer, service)

	return &Server{
		grpcServer: grpcServer,
		socketPath: cfg.SocketPath,
		manager:    manager,
		service:    service,
		logger:     logger,
	}
}

// Start starts the gRPC server on a Unix domain socket
func (s *Server) Start() error {
	// Remove existing socket file if it exists
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Create Unix socket listener
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	s.listener = listener

	s.logger.Info("Starting gRPC server on Unix socket", loggerv2.String("socket", s.socketPath))
	return s.grpcServer.Serve(listener)
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down gRPC server")

	// Graceful stop with timeout
	done := make(chan struct{})
	go func() {
		s.grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("gRPC server stopped gracefully")
	case <-ctx.Done():
		s.logger.Warn("gRPC server shutdown timed out, forcing stop")
		s.grpcServer.Stop()
	}

	// Clean up socket file
	if s.socketPath != "" {
		_ = os.Remove(s.socketPath)
	}

	return nil
}

// GetManager returns the agent manager
func (s *Server) GetManager() *AgentManager {
	return s.manager
}

// GetService returns the agent service (for advanced use cases)
func (s *Server) GetService() *AgentService {
	return s.service
}
