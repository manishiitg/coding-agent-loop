/**
 * OAuth API Service
 * Handles OAuth authentication flows for MCP servers
 */

export interface OAuthStartRequest {
  server_name: string;
  client_id?: string;
}

export interface OAuthStartResponse {
  server_name: string;
  auth_url: string;
  state: string;
  message: string;
}

export interface OAuthDiscoveryResponse {
  status: 'needs_client_id';
  server_name: string;
  auth_url?: string;
  token_url?: string;
  resource?: string;
  scopes_supported?: string[];
  message: string;
}

export interface OAuthStatusResponse {
  server_name: string;
  valid: boolean;
  expires_in: string;
  token_path: string;
}

export interface OAuthLogoutRequest {
  server_name: string;
}

export class OAuthApi {
  private baseUrl: string;

  constructor(baseUrl: string = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8000') {
    this.baseUrl = baseUrl;
  }

  /**
   * Start OAuth flow for a server
   * Returns OAuthDiscoveryResponse if server needs a client_id, otherwise OAuthStartResponse
   */
  async startOAuthFlow(serverName: string, clientId?: string): Promise<OAuthStartResponse | OAuthDiscoveryResponse> {
    const body: OAuthStartRequest = { server_name: serverName };
    if (clientId) {
      body.client_id = clientId;
    }

    const response = await fetch(`${this.baseUrl}/api/oauth/start`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });

    if (!response.ok) {
      const error = await response.text();
      throw new Error(`OAuth start failed: ${error}`);
    }

    return response.json();
  }

  /**
   * Get OAuth token status for a server
   */
  async getOAuthStatus(serverName: string): Promise<OAuthStatusResponse> {
    const response = await fetch(
      `${this.baseUrl}/api/oauth/status?server_name=${encodeURIComponent(serverName)}`
    );

    if (!response.ok) {
      const error = await response.text();
      throw new Error(`Failed to get OAuth status: ${error}`);
    }

    return response.json();
  }

  /**
   * Logout from OAuth (remove token)
   */
  async logout(serverName: string): Promise<void> {
    const response = await fetch(`${this.baseUrl}/api/oauth/logout`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ server_name: serverName }),
    });

    if (!response.ok) {
      const error = await response.text();
      throw new Error(`OAuth logout failed: ${error}`);
    }
  }
}

// Export a default instance
export const oauthApi = new OAuthApi(import.meta.env.VITE_API_BASE_URL || 'http://localhost:8000');
