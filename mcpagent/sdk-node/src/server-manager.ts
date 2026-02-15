import { spawn, ChildProcess } from 'child_process';
import { existsSync, unlinkSync } from 'fs';
import path from 'path';
import os from 'os';
import { config as loadEnv } from 'dotenv';

/**
 * Options for the Go server manager
 */
export interface ServerManagerOptions {
  /** Unix socket path for gRPC (default: /tmp/mcpagent-grpc-{pid}.sock) */
  socketPath?: string;
  /** Path to MCP servers config file */
  mcpConfigPath?: string;
  /** Path to Go project root (default: auto-detect) */
  goProjectPath?: string;
  /** Log level for Go server */
  logLevel?: 'debug' | 'info' | 'warn' | 'error';
  /** Timeout for server startup in ms (default: 30000) */
  startupTimeout?: number;
}

/**
 * Manages the lifecycle of the Go gRPC server.
 * Automatically starts/stops the Go server as needed.
 * The Go server is linked to the Node.js process - when Node exits, Go exits too.
 *
 * Communication happens via Unix domain sockets for security and performance.
 */
export class ServerManager {
  private process: ChildProcess | null = null;
  private socketPath: string;
  private mcpConfigPath: string;
  private goProjectPath: string;
  private logLevel: string;
  private startupTimeout: number;
  private isRunning: boolean = false;
  private cleanupRegistered: boolean = false;

  constructor(options: ServerManagerOptions = {}) {
    // Generate unique socket path based on PID
    const tmpDir = os.tmpdir();
    this.socketPath = options.socketPath ?? path.join(tmpDir, `mcpagent-grpc-${process.pid}.sock`);
    this.mcpConfigPath = options.mcpConfigPath ?? 'mcp_servers.json';
    this.goProjectPath = options.goProjectPath ?? this.detectGoProjectPath();
    this.logLevel = options.logLevel ?? 'info';
    this.startupTimeout = options.startupTimeout ?? 30000;

    // Load .env file from nodejs directory
    const envPath = path.join(__dirname, '..', '.env');
    if (existsSync(envPath)) {
      loadEnv({ path: envPath });
    }

    // Register cleanup handlers to kill Go server when Node.js exits
    this.registerCleanupHandlers();
  }

  /**
   * Register handlers to clean up Go server on Node.js exit
   */
  private registerCleanupHandlers(): void {
    if (this.cleanupRegistered) return;
    this.cleanupRegistered = true;

    const cleanup = () => {
      if (this.process) {
        this.process.kill('SIGTERM');
        this.process = null;
      }
      // Clean up socket file
      try {
        if (existsSync(this.socketPath)) {
          unlinkSync(this.socketPath);
        }
      } catch {
        // Ignore cleanup errors
      }
    };

    // Normal exit
    process.on('exit', cleanup);

    // Ctrl+C
    process.on('SIGINT', () => {
      cleanup();
      process.exit(0);
    });

    // Kill signal
    process.on('SIGTERM', () => {
      cleanup();
      process.exit(0);
    });

    // Uncaught exceptions
    process.on('uncaughtException', (err) => {
      console.error('Uncaught exception:', err);
      cleanup();
      process.exit(1);
    });

    // Unhandled promise rejections
    process.on('unhandledRejection', (reason) => {
      console.error('Unhandled rejection:', reason);
      cleanup();
      process.exit(1);
    });
  }

  /**
   * Start the Go gRPC server
   * @returns The gRPC socket path
   */
  async start(): Promise<string> {
    if (this.isRunning) {
      return this.socketPath;
    }

    // Check if server is already running on this socket
    if (await this.isServerHealthy()) {
      this.isRunning = true;
      return this.socketPath;
    }

    // Clean up stale socket file if it exists
    try {
      if (existsSync(this.socketPath)) {
        unlinkSync(this.socketPath);
      }
    } catch {
      // Ignore cleanup errors
    }

    // Verify Go project exists
    const mainGoPath = path.join(this.goProjectPath, 'cmd', 'server', 'main.go');
    if (!existsSync(mainGoPath)) {
      throw new Error(
        `Go server not found at ${mainGoPath}. ` +
        `Please ensure the mcpagent Go project is available.`
      );
    }

    // Resolve MCP config path
    let configPath = this.mcpConfigPath;
    if (!path.isAbsolute(configPath)) {
      configPath = path.join(this.goProjectPath, configPath);
    }

    // Start the Go gRPC server
    return new Promise((resolve, reject) => {
      const args = [
        'run', mainGoPath,
        '--socket', this.socketPath,
        '--config', configPath,
        '--log-level', this.logLevel,
        '--parent-pid', String(process.pid), // Pass parent PID so Go can exit if parent dies
      ];

      this.process = spawn('go', args, {
        cwd: this.goProjectPath,
        stdio: ['ignore', 'pipe', 'pipe'],
        detached: false,
        env: { ...process.env }, // Inherit environment variables
      });

      let startupOutput = '';
      const timeout = setTimeout(() => {
        this.stop();
        reject(new Error(
          `Go server failed to start within ${this.startupTimeout}ms. Output:\n${startupOutput}`
        ));
      }, this.startupTimeout);

      this.process.stdout?.on('data', (data) => {
        startupOutput += data.toString();
      });

      this.process.stderr?.on('data', (data) => {
        startupOutput += data.toString();
      });

      this.process.on('error', (err) => {
        clearTimeout(timeout);
        reject(new Error(`Failed to start Go server: ${err.message}`));
      });

      this.process.on('exit', (code) => {
        if (!this.isRunning) {
          clearTimeout(timeout);
          reject(new Error(
            `Go server exited with code ${code} before becoming healthy. Output:\n${startupOutput}`
          ));
        }
      });

      // Poll for socket file existence (gRPC doesn't have HTTP health endpoint)
      const pollHealth = async () => {
        try {
          if (await this.isServerHealthy()) {
            clearTimeout(timeout);
            this.isRunning = true;
            resolve(this.socketPath);
            return;
          }
        } catch {
          // Server not ready yet
        }
        setTimeout(pollHealth, 100);
      };

      setTimeout(pollHealth, 200);
    });
  }

  /**
   * Stop the Go server
   */
  async stop(): Promise<void> {
    if (this.process) {
      this.process.kill('SIGTERM');

      // Wait for graceful shutdown
      await new Promise<void>((resolve) => {
        const timeout = setTimeout(() => {
          this.process?.kill('SIGKILL');
          resolve();
        }, 5000);

        this.process?.on('exit', () => {
          clearTimeout(timeout);
          resolve();
        });
      });

      this.process = null;
    }
    this.isRunning = false;

    // Clean up socket file
    try {
      if (existsSync(this.socketPath)) {
        unlinkSync(this.socketPath);
      }
    } catch {
      // Ignore cleanup errors
    }
  }

  /**
   * Get the gRPC Unix socket path
   */
  getSocketPath(): string {
    return this.socketPath;
  }

  /**
   * Check if the server is running and healthy
   * For gRPC, we just check if the socket file exists
   */
  async isServerHealthy(): Promise<boolean> {
    return existsSync(this.socketPath);
  }

  /**
   * Detect the Go project path relative to this package
   */
  private detectGoProjectPath(): string {
    // Try relative paths from this package
    const candidates = [
      // When installed as npm package, Go project should be sibling
      path.join(__dirname, '..', '..', '..'),
      // Development: nodejs is inside mcpagent
      path.join(__dirname, '..', '..'),
      // Custom location via env
      process.env.MCPAGENT_GO_PATH,
    ].filter(Boolean) as string[];

    for (const candidate of candidates) {
      const mainGo = path.join(candidate, 'cmd', 'server', 'main.go');
      if (existsSync(mainGo)) {
        return candidate;
      }
    }

    // Default to parent directory (most common case)
    return path.join(__dirname, '..', '..');
  }
}
