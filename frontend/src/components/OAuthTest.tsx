/**
 * OAuth Test Component
 * Simple UI for testing OAuth flows with MCP servers
 */

import React, { useState, useEffect } from 'react';
import { oauthApi } from '../services/oauthApi';

interface OAuthTestProps {
  serverName?: string;
}

export const OAuthTest: React.FC<OAuthTestProps> = ({ serverName = 'Notion' }) => {
  const [tokenStatus, setTokenStatus] = useState<{
    valid: boolean;
    expiresIn?: string;
    tokenPath?: string;
  } | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);

  const checkTokenStatus = React.useCallback(async () => {
    try {
      const status = await oauthApi.getOAuthStatus(serverName);
      setTokenStatus(status);
      setError(null);
    } catch {
      // Token might not exist yet, which is fine
      setTokenStatus(null);
    }
  }, [serverName]);

  // Check token status on mount and every 5 seconds
  useEffect(() => {
    checkTokenStatus();
    const interval = setInterval(checkTokenStatus, 5000);
    return () => clearInterval(interval);
  }, [checkTokenStatus]);

  const handleLogin = async () => {
    setLoading(true);
    setError(null);
    setMessage(null);

    try {
      const response = await oauthApi.startOAuthFlow(serverName);
      setMessage(response.message || 'OAuth flow started - check your browser!');

      // Poll for token status every 2 seconds
      const pollInterval = setInterval(async () => {
        try {
          const status = await oauthApi.getOAuthStatus(serverName);
          if (status.valid) {
            clearInterval(pollInterval);
            setTokenStatus(status);
            setMessage('✅ Successfully authenticated!');
            setLoading(false);
          }
        } catch {
          // Still waiting for token
        }
      }, 2000);

      // Stop polling after 5 minutes
      setTimeout(() => {
        clearInterval(pollInterval);
        setLoading(false);
      }, 5 * 60 * 1000);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'OAuth login failed');
      setLoading(false);
    }
  };

  const handleLogout = async () => {
    setLoading(true);
    setError(null);
    setMessage(null);

    try {
      await oauthApi.logout(serverName);
      setTokenStatus(null);
      setMessage('Successfully logged out');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Logout failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div style={{
      maxWidth: '600px',
      margin: '40px auto',
      padding: '30px',
      border: '1px solid #e0e0e0',
      borderRadius: '8px',
      fontFamily: 'system-ui, -apple-system, sans-serif'
    }}>
      <h2 style={{ marginTop: 0, marginBottom: '20px' }}>
        OAuth Test - {serverName}
      </h2>

      {/* Token Status */}
      <div style={{
        padding: '15px',
        backgroundColor: tokenStatus?.valid ? '#e8f5e9' : '#fff3e0',
        borderRadius: '6px',
        marginBottom: '20px'
      }}>
        <div style={{ fontWeight: 600, marginBottom: '8px' }}>
          Status: {tokenStatus?.valid ? '✅ Authenticated' : '❌ Not Authenticated'}
        </div>
        {tokenStatus?.valid && (
          <>
            <div style={{ fontSize: '14px', color: '#666' }}>
              Expires in: {tokenStatus.expiresIn}
            </div>
            <div style={{ fontSize: '12px', color: '#999', marginTop: '4px' }}>
              Token: {tokenStatus.tokenPath}
            </div>
          </>
        )}
      </div>

      {/* Messages */}
      {message && (
        <div style={{
          padding: '12px',
          backgroundColor: '#e3f2fd',
          borderRadius: '6px',
          marginBottom: '15px',
          fontSize: '14px'
        }}>
          {message}
        </div>
      )}

      {/* Errors */}
      {error && (
        <div style={{
          padding: '12px',
          backgroundColor: '#ffebee',
          color: '#c62828',
          borderRadius: '6px',
          marginBottom: '15px',
          fontSize: '14px'
        }}>
          ❌ {error}
        </div>
      )}

      {/* Action Buttons */}
      <div style={{ display: 'flex', gap: '10px' }}>
        {!tokenStatus?.valid ? (
          <button
            onClick={handleLogin}
            disabled={loading}
            style={{
              padding: '12px 24px',
              backgroundColor: loading ? '#ccc' : '#1976d2',
              color: 'white',
              border: 'none',
              borderRadius: '6px',
              fontSize: '14px',
              fontWeight: 600,
              cursor: loading ? 'not-allowed' : 'pointer',
              flex: 1
            }}
          >
            {loading ? 'Authenticating...' : 'Login with OAuth'}
          </button>
        ) : (
          <button
            onClick={handleLogout}
            disabled={loading}
            style={{
              padding: '12px 24px',
              backgroundColor: loading ? '#ccc' : '#d32f2f',
              color: 'white',
              border: 'none',
              borderRadius: '6px',
              fontSize: '14px',
              fontWeight: 600,
              cursor: loading ? 'not-allowed' : 'pointer',
              flex: 1
            }}
          >
            {loading ? 'Logging out...' : 'Logout'}
          </button>
        )}

        <button
          onClick={checkTokenStatus}
          disabled={loading}
          style={{
            padding: '12px 24px',
            backgroundColor: 'white',
            color: '#1976d2',
            border: '1px solid #1976d2',
            borderRadius: '6px',
            fontSize: '14px',
            fontWeight: 600,
            cursor: loading ? 'not-allowed' : 'pointer'
          }}
        >
          Refresh Status
        </button>
      </div>

      {/* Instructions */}
      <div style={{
        marginTop: '30px',
        padding: '15px',
        backgroundColor: '#f5f5f5',
        borderRadius: '6px',
        fontSize: '13px',
        lineHeight: '1.6'
      }}>
        <div style={{ fontWeight: 600, marginBottom: '8px' }}>How to test:</div>
        <ol style={{ margin: 0, paddingLeft: '20px' }}>
          <li>Make sure the backend server is running (<code>go run main.go server</code>)</li>
          <li>Click "Login with OAuth"</li>
          <li>A browser window will open automatically</li>
          <li>Complete the authentication in the browser</li>
          <li>Return here to see the updated status</li>
        </ol>
      </div>
    </div>
  );
};

export default OAuthTest;
