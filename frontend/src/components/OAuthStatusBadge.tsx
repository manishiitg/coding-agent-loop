/**
 * OAuth Status Badge Component
 * Shows OAuth authentication status for MCP servers
 */

import React, { useState, useEffect, useRef } from 'react';
import { ShieldCheck, ShieldAlert, Loader2, RefreshCw, Key, X } from 'lucide-react';
import { oauthApi } from '../services/oauthApi';
import type { OAuthDiscoveryResponse } from '../services/oauthApi';

interface OAuthStatusBadgeProps {
  serverName: string;
  requiresOAuth?: boolean; // Auto-detected from server discovery
  onAuthChange?: (valid: boolean) => void;
}

export const OAuthStatusBadge: React.FC<OAuthStatusBadgeProps> = ({
  serverName,
  requiresOAuth,
  onAuthChange
}) => {
  const [tokenValid, setTokenValid] = useState<boolean>(false);
  const [expiresIn, setExpiresIn] = useState<string>('');
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [hasOAuth, setHasOAuth] = useState<boolean | null>(null);
  const prevTokenValidRef = useRef<boolean | null>(null);

  // Client ID dialog state
  const [showClientIdDialog, setShowClientIdDialog] = useState(false);
  const [clientIdInput, setClientIdInput] = useState('');
  const [discoveryInfo, setDiscoveryInfo] = useState<OAuthDiscoveryResponse | null>(null);

  // Check token status on mount and periodically
  const checkTokenStatus = React.useCallback(async () => {
    try {
      console.log(`[OAuthStatusBadge] Checking status for ${serverName}...`);
      const status = await oauthApi.getOAuthStatus(serverName);
      console.log(`[OAuthStatusBadge] Status for ${serverName}:`, status);

      // Only call onAuthChange when validity actually changes (use ref to avoid stale closure)
      const prevValid = prevTokenValidRef.current;
      const validityChanged = prevValid !== null && prevValid !== status.valid;

      setTokenValid(status.valid);
      setExpiresIn(status.expires_in);
      setHasOAuth(true);
      prevTokenValidRef.current = status.valid;

      // Only trigger refresh when transitioning from invalid to valid
      if (validityChanged && status.valid) {
        console.log(`[OAuthStatusBadge] Auth changed to valid for ${serverName}, triggering refresh`);
        onAuthChange?.(status.valid);
      }
    } catch (error) {
      console.log(`[OAuthStatusBadge] Error checking status for ${serverName}:`, error);
      // If not explicitly told server has OAuth, check if auto-detected
      if (requiresOAuth === undefined) {
        setHasOAuth(false);
      }
      setTokenValid(false);
      prevTokenValidRef.current = false;
    }
  }, [serverName, requiresOAuth, onAuthChange]);

  useEffect(() => {
    // If requiresOAuth is explicitly passed (from auto-discovery), use it immediately
    if (requiresOAuth !== undefined) {
      setHasOAuth(requiresOAuth);
    }

    checkTokenStatus();
    const interval = setInterval(checkTokenStatus, 10000); // Every 10 seconds
    return () => clearInterval(interval);
  }, [serverName, requiresOAuth, checkTokenStatus]);

  const handleManualRefresh = async () => {
    setRefreshing(true);
    console.log(`[OAuthStatusBadge] Manual refresh triggered for ${serverName}`);
    try {
      await checkTokenStatus();
    } finally {
      setRefreshing(false);
    }
  };

  const handleLogin = async (clientId?: string) => {
    setLoading(true);
    console.log(`[OAuthStatusBadge] Starting OAuth login for ${serverName}${clientId ? ' with client_id' : ''}`);
    try {
      // Start OAuth flow and get authorization URL
      const response = await oauthApi.startOAuthFlow(serverName, clientId);
      console.log(`[OAuthStatusBadge] OAuth flow response for ${serverName}:`, response);

      // Check if the server needs a client_id
      if ('status' in response && response.status === 'needs_client_id') {
        console.log(`[OAuthStatusBadge] Server ${serverName} needs client_id`);
        setDiscoveryInfo(response as OAuthDiscoveryResponse);
        setShowClientIdDialog(true);
        setLoading(false);
        return;
      }

      // Normal flow - open browser with authorization URL
      const startResponse = response as { auth_url: string; state: string };
      if (startResponse.auth_url) {
        console.log(`[OAuthStatusBadge] Opening auth URL for ${serverName}`);
        window.open(startResponse.auth_url, '_blank');
      }

      // Poll for completion
      let pollCount = 0;
      const pollInterval = setInterval(async () => {
        pollCount++;
        try {
          console.log(`[OAuthStatusBadge] Polling OAuth status for ${serverName} (attempt ${pollCount})`);
          const status = await oauthApi.getOAuthStatus(serverName);
          console.log(`[OAuthStatusBadge] Poll result for ${serverName}:`, status);
          if (status.valid) {
            console.log(`[OAuthStatusBadge] OAuth completed for ${serverName}!`);
            clearInterval(pollInterval);
            const wasInvalid = prevTokenValidRef.current === false || prevTokenValidRef.current === null;
            setTokenValid(true);
            setExpiresIn(status.expires_in);
            setLoading(false);
            prevTokenValidRef.current = true;
            // Only trigger refresh if transitioning from invalid to valid
            if (wasInvalid) {
              console.log(`[OAuthStatusBadge] Triggering onAuthChange for ${serverName}`);
              onAuthChange?.(true);
            }
          }
        } catch (err) {
          console.log(`[OAuthStatusBadge] Poll error for ${serverName}:`, err);
          // Still waiting
        }
      }, 2000);

      // Stop polling after 5 minutes
      setTimeout(() => {
        console.log(`[OAuthStatusBadge] Polling timeout for ${serverName}`);
        clearInterval(pollInterval);
        setLoading(false);
      }, 5 * 60 * 1000);
    } catch (error) {
      console.error('[OAuthStatusBadge] OAuth login failed:', error);
      setLoading(false);
    }
  };

  const handleClientIdSubmit = () => {
    if (!clientIdInput.trim()) return;
    setShowClientIdDialog(false);
    setDiscoveryInfo(null);
    handleLogin(clientIdInput.trim());
    setClientIdInput('');
  };

  const handleClientIdCancel = () => {
    setShowClientIdDialog(false);
    setClientIdInput('');
    setDiscoveryInfo(null);
  };

  const handleLogout = async () => {
    setLoading(true);
    try {
      await oauthApi.logout(serverName);
      setTokenValid(false);
      setExpiresIn('');
      onAuthChange?.(false);
    } catch (error) {
      console.error('OAuth logout failed:', error);
    } finally {
      setLoading(false);
    }
  };

  // Don't render if server doesn't have OAuth
  if (hasOAuth === false) {
    return null;
  }

  // Loading state while checking
  if (hasOAuth === null) {
    return (
      <div className="flex items-center gap-1 px-2 py-1 text-xs bg-gray-100 dark:bg-gray-700 rounded">
        <Loader2 className="w-3 h-3 animate-spin" />
      </div>
    );
  }

  // Client ID dialog modal
  const clientIdDialog = showClientIdDialog && (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl p-6 max-w-md w-full mx-4">
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-2">
            <Key className="w-5 h-5 text-orange-500" />
            <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
              Client ID Required
            </h3>
          </div>
          <button
            onClick={handleClientIdCancel}
            className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 rounded"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <p className="text-sm text-gray-600 dark:text-gray-400 mb-4">
          {discoveryInfo?.message || `This server does not support automatic client registration. Please enter your OAuth App client ID.`}
        </p>

        {discoveryInfo?.scopes_supported && discoveryInfo.scopes_supported.length > 0 && (
          <div className="mb-4 p-2 bg-blue-50 dark:bg-blue-900/20 rounded text-xs text-blue-700 dark:text-blue-300">
            <span className="font-medium">Discovered scopes:</span>{' '}
            {discoveryInfo.scopes_supported.join(', ')}
          </div>
        )}

        {discoveryInfo?.resource && (
          <div className="mb-4 p-2 bg-gray-50 dark:bg-gray-700/50 rounded text-xs text-gray-600 dark:text-gray-400">
            <span className="font-medium">Resource:</span> {discoveryInfo.resource}
          </div>
        )}

        <div className="mb-4">
          <label htmlFor="client-id-input" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
            Client ID
          </label>
          <input
            id="client-id-input"
            type="text"
            value={clientIdInput}
            onChange={(e) => setClientIdInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleClientIdSubmit()}
            placeholder="Enter your OAuth client_id"
            className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            autoFocus
          />
        </div>

        <div className="flex justify-end gap-2">
          <button
            onClick={handleClientIdCancel}
            className="px-4 py-2 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 rounded-md transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={handleClientIdSubmit}
            disabled={!clientIdInput.trim()}
            className="px-4 py-2 text-sm font-medium bg-blue-600 text-white rounded-md hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
          >
            Continue
          </button>
        </div>
      </div>
    </div>
  );

  if (!tokenValid) {
    return (
      <>
        {clientIdDialog}
        <div className="flex items-center gap-1">
          <button
            onClick={() => handleLogin()}
            disabled={loading}
            className="flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium bg-orange-100 dark:bg-orange-900/30 text-orange-700 dark:text-orange-300 hover:bg-orange-200 dark:hover:bg-orange-900/50 rounded-md transition-colors disabled:opacity-50"
            title="Click to authenticate with OAuth"
          >
            {loading ? (
              <>
                <Loader2 className="w-3 h-3 animate-spin" />
                <span>Authenticating...</span>
              </>
            ) : (
              <>
                <ShieldAlert className="w-3 h-3" />
                <span>Login</span>
              </>
            )}
          </button>
          <button
            onClick={handleManualRefresh}
            disabled={refreshing || loading}
            className="p-1 text-xs text-gray-500 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20 rounded transition-colors disabled:opacity-50"
            title="Refresh status"
          >
            <RefreshCw className={`w-3 h-3 ${refreshing ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </>
    );
  }

  return (
    <div className="flex items-center gap-1.5">
      <div className="flex items-center gap-1 px-2 py-1 text-xs bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 rounded-md">
        <ShieldCheck className="w-3 h-3" />
        <span>OAuth</span>
      </div>
      <button
        onClick={handleManualRefresh}
        disabled={refreshing}
        className="p-1 text-xs text-gray-500 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/20 rounded transition-colors disabled:opacity-50"
        title={`Refresh status - Expires: ${expiresIn}`}
      >
        <RefreshCw className={`w-3 h-3 ${refreshing ? 'animate-spin' : ''}`} />
      </button>
      <button
        onClick={handleLogout}
        disabled={loading}
        className="px-2 py-1 text-xs text-gray-600 dark:text-gray-400 hover:text-red-600 dark:hover:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 rounded transition-colors disabled:opacity-50"
        title="Logout"
      >
        {loading ? '...' : '✕'}
      </button>
    </div>
  );
};

export default OAuthStatusBadge;
