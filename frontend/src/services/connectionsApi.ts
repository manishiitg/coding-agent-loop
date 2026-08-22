/**
 * Connections API Service
 *
 * The user-facing layer over MCP servers. A "connection" is an approved
 * account (Notion, GitHub, Google Workspace); MCP is the transport underneath.
 * Every failure here arrives already translated into a recovery path, so the
 * UI never has to show a raw 401.
 */

import { getApiBaseUrl, getAuthToken } from './api';

function getAuthHeaders(): HeadersInit {
  const headers: HeadersInit = { 'Content-Type': 'application/json' };
  const token = getAuthToken();
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  return headers;
}

/** How a catalog entry authenticates. */
export type ConnectionAuthType = 'dcr' | 'oauth_app' | 'token';

/** How a server is reached — shown as the "Type" column. */
export type ConnectionTransport = 'web' | 'local';

/** Health of a provisioned connection. */
export type ConnectionHealth =
  | 'connected'
  | 'needs_reconnect'
  | 'setup_required'
  | 'not_connected';

/** What the user can do about a failure. */
export type RecoveryAction =
  | 'reconnect'
  | 'retry'
  | 'connect'
  | 'enter_token'
  | 'open_advanced'
  | 'contact_admin';

/**
 * A transport failure rewritten as something a person can act on. `raw` keeps
 * the original text for the Advanced section.
 */
export interface FriendlyError {
  code: string;
  title: string;
  message: string;
  action?: RecoveryAction;
  raw?: string;
}

export interface CatalogEntry {
  id: string;
  server_name: string;
  name: string;
  tagline?: string;
  description?: string;
  category?: string;
  icon?: string;
  brand_color?: string;
  docs_url?: string;
  status?: 'available' | 'coming_soon';
  auth: ConnectionAuthType;
  transport: ConnectionTransport;
  url?: string;
  command?: string;
  capabilities?: string[];
  sensitive_actions?: string[];
  setup_hint?: string;
  token_label?: string;
  token_placeholder?: string;
  token_env_var?: string;
  extra_env?: Record<string, string>;
  /** Computed server-side: admin has not supplied credentials yet. */
  setup_required: boolean;
}

export interface Connection {
  id: string;
  server_name: string;
  name: string;
  icon?: string;
  brand_color?: string;
  auth: ConnectionAuthType;
  transport: ConnectionTransport;
  health: ConnectionHealth;
  expires_in?: string;
  /** True for servers added via Custom MCP rather than the catalog. */
  custom: boolean;
  error?: FriendlyError;
}

export interface ConnectionsSummary {
  connected: number;
  needs_attention: number;
  total: number;
}

export interface ConnectionsListResponse {
  connections: Connection[];
  summary: ConnectionsSummary;
}

export interface ConnectResult {
  /** 'connected' for token auth; 'oauth' when the user must approve in a popup. */
  status: 'connected' | 'oauth' | 'needs_client_id';
  server_name: string;
  auth_url?: string;
  state?: string;
  message?: string;
  /** Present when the provider has no dynamic client registration. */
  scopes_supported?: string[];
  resource?: string;
}

export interface TestResult {
  status: string;
  server_name: string;
  tool_count: number;
  tools: string[];
  message: string;
}

export interface ConnectPayload {
  token?: string;
  client_id?: string;
  env?: Record<string, string>;
}

/**
 * Thrown for every non-2xx response. Carries the already-translated
 * FriendlyError so callers can render a recovery path instead of a status code.
 */
export class ConnectionError extends Error {
  readonly friendly: FriendlyError;

  constructor(friendly: FriendlyError) {
    super(friendly.message);
    this.name = 'ConnectionError';
    this.friendly = friendly;
  }
}

/**
 * Last-resort translation for responses that never reached the connections
 * handlers (proxy errors, network failures). Keeps the promise that the UI
 * always has a title/message/action to render.
 */
function fallbackFriendly(status: number, raw: string): FriendlyError {
  if (status === 401 || status === 403) {
    return {
      code: 'unauthorized',
      title: 'Your session expired',
      message: 'Sign in again to manage connections.',
      action: 'reconnect',
      raw,
    };
  }
  if (status === 0) {
    return {
      code: 'unreachable',
      title: 'Could not reach the server',
      message: 'Check your network connection and try again.',
      action: 'retry',
      raw,
    };
  }
  return {
    code: 'unknown',
    title: 'Something went wrong',
    message: 'The request could not be completed. Open Advanced for details.',
    action: 'open_advanced',
    raw,
  };
}

async function parseError(response: Response): Promise<ConnectionError> {
  const text = await response.text();
  try {
    const body = JSON.parse(text);
    if (body?.error?.title) {
      return new ConnectionError(body.error as FriendlyError);
    }
  } catch {
    // Not JSON — fall through to the generic mapping below.
  }
  return new ConnectionError(fallbackFriendly(response.status, text));
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`${getApiBaseUrl()}${path}`, {
      ...init,
      headers: getAuthHeaders(),
    });
  } catch (err) {
    throw new ConnectionError(fallbackFriendly(0, String(err)));
  }

  if (!response.ok) {
    throw await parseError(response);
  }
  return response.json() as Promise<T>;
}

export class ConnectionsApi {
  /** Curated integration catalog. */
  async getCatalog(): Promise<{ version: number; integrations: CatalogEntry[] }> {
    return request('/api/connections/catalog');
  }

  /** Connections the user has provisioned, with health. */
  async list(): Promise<ConnectionsListResponse> {
    return request('/api/connections');
  }

  /**
   * Provision an integration and begin authentication.
   * For OAuth entries the caller must open `auth_url` and then poll `list()`.
   */
  async connect(id: string, payload: ConnectPayload = {}): Promise<ConnectResult> {
    const raw = await request<Record<string, unknown>>(
      `/api/connections/${encodeURIComponent(id)}/connect`,
      { method: 'POST', body: JSON.stringify(payload) }
    );

    // The connect endpoint delegates to the OAuth start handler, so normalise
    // its three possible shapes into one discriminated result.
    if (raw.status === 'needs_client_id') {
      return { ...raw, status: 'needs_client_id' } as ConnectResult;
    }
    if (raw.auth_url) {
      return { ...raw, status: 'oauth' } as ConnectResult;
    }
    return { ...raw, status: 'connected' } as ConnectResult;
  }

  /** Remove the stored token but keep the connection, so Reconnect is one click. */
  async disconnect(id: string): Promise<{ status: string; message: string }> {
    return request(`/api/connections/${encodeURIComponent(id)}/disconnect`, {
      method: 'POST',
    });
  }

  /** Connect and list tools so the user gets a concrete "it works" signal. */
  async test(id: string): Promise<TestResult> {
    return request(`/api/connections/${encodeURIComponent(id)}/test`, {
      method: 'POST',
    });
  }
}

export const connectionsApi = new ConnectionsApi();
