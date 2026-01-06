/**
 * OAuth Status Badge Component
 * Shows OAuth authentication status for MCP servers
 */

import React, { useState, useEffect, useRef } from 'react';
import { ShieldCheck, ShieldAlert, Loader2, RefreshCw } from 'lucide-react';
import { oauthApi } from '../services/oauthApi';

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

  // Check token status on mount and periodically
  useEffect(() => {
    // If requiresOAuth is explicitly passed (from auto-discovery), use it immediately
    if (requiresOAuth !== undefined) {
      setHasOAuth(requiresOAuth);
    }

    checkTokenStatus();
    const interval = setInterval(checkTokenStatus, 10000); // Every 10 seconds
    return () => clearInterval(interval);
  }, [serverName, requiresOAuth]);

  const checkTokenStatus = async () => {
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
  };

  const handleManualRefresh = async () => {
    setRefreshing(true);
    console.log(`[OAuthStatusBadge] Manual refresh triggered for ${serverName}`);
    try {
      await checkTokenStatus();
    } finally {
      setRefreshing(false);
    }
  };

  const handleLogin = async () => {
    setLoading(true);
    console.log(`[OAuthStatusBadge] Starting OAuth login for ${serverName}`);
    try {
      // Start OAuth flow and get authorization URL
      const response = await oauthApi.startOAuthFlow(serverName);
      console.log(`[OAuthStatusBadge] OAuth flow started for ${serverName}:`, response);

      // Open browser with authorization URL
      if (response.auth_url) {
        console.log(`[OAuthStatusBadge] Opening auth URL for ${serverName}`);
        window.open(response.auth_url, '_blank');
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

  if (!tokenValid) {
    return (
      <div className="flex items-center gap-1">
        <button
          onClick={handleLogin}
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
