import { useState, useEffect } from 'react'
import { useAuthStore } from '../stores/useAuthStore'
import { Button } from '../components/ui/Button'
import { Input } from '../components/ui/Input'
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from '../components/ui/Card'
import { Alert, AlertDescription } from '../components/ui/alert'
import { Loader2, Bot, KeyRound, Mail, Lock, AlertCircle } from 'lucide-react'
import type { AuthProvider } from '../services/api'

// Google icon SVG component
const GoogleIcon = () => (
  <svg className="w-5 h-5" viewBox="0 0 24 24">
    <path
      fill="#4285F4"
      d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"
    />
    <path
      fill="#34A853"
      d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"
    />
    <path
      fill="#FBBC05"
      d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"
    />
    <path
      fill="#EA4335"
      d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"
    />
  </svg>
)

// AWS Cognito icon
const CognitoIcon = () => (
  <svg className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor">
    <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z" />
  </svg>
)

// Provider display configuration
const providerConfig: Record<string, {
  displayName: string
  icon: React.ReactNode
  description: string
  buttonClass?: string
}> = {
  simple: {
    displayName: 'Username & Password',
    icon: <KeyRound className="w-5 h-5" />,
    description: 'Sign in with your credentials'
  },
  cognito: {
    displayName: 'Continue with Google',
    icon: <GoogleIcon />,
    description: 'Sign in with your Google Workspace account',
    buttonClass: 'bg-white hover:bg-gray-50 text-gray-700 border border-gray-300 dark:bg-gray-800 dark:hover:bg-gray-700 dark:text-gray-200 dark:border-gray-600'
  },
  supabase: {
    displayName: 'Sign in with Supabase',
    icon: <CognitoIcon />,
    description: 'Sign in with Supabase'
  }
}

export function Login() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [selectedProvider, setSelectedProvider] = useState<string | null>(null)
  const {
    login,
    loginWithOAuth,
    isLoading,
    error,
    clearError,
    providers,
    checkAuthMode,
    isMultiUserModeChecked
  } = useAuthStore()

  // Check auth mode on mount to get available providers
  useEffect(() => {
    if (!isMultiUserModeChecked) {
      checkAuthMode()
    }
  }, [checkAuthMode, isMultiUserModeChecked])

  // Auto-select provider if only one is available
  useEffect(() => {
    if (providers.length === 1) {
      setSelectedProvider(providers[0].name)
    }
  }, [providers])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    clearError()
    try {
      await login(username, password, selectedProvider || undefined)
    } catch (err) {
      console.error('Login failed:', err)
    }
  }

  const handleOAuthLogin = async (provider: AuthProvider) => {
    clearError()
    try {
      await loginWithOAuth(provider.name)
    } catch (err) {
      console.error('OAuth login failed:', err)
    }
  }

  // Get credentials-based providers
  const credentialsProviders = providers.filter(p => p.type === 'credentials')
  const oauthProviders = providers.filter(p => p.type === 'oauth')

  // Check if we should show credentials form
  const showCredentialsForm = selectedProvider &&
    credentialsProviders.some(p => p.name === selectedProvider)

  // Loading state
  if (!isMultiUserModeChecked) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-background to-muted">
        <div className="flex flex-col items-center gap-4">
          <Loader2 className="w-8 h-8 animate-spin text-primary" />
          <p className="text-muted-foreground">Loading...</p>
        </div>
      </div>
    )
  }

  // No providers configured
  if (providers.length === 0) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-background to-muted p-4">
        <Card className="w-full max-w-md">
          <CardHeader className="text-center">
            <div className="mx-auto w-16 h-16 bg-destructive/10 rounded-full flex items-center justify-center mb-4">
              <AlertCircle className="w-8 h-8 text-destructive" />
            </div>
            <CardTitle>Configuration Error</CardTitle>
            <CardDescription>
              No authentication providers have been configured. Please contact your administrator.
            </CardDescription>
          </CardHeader>
        </Card>
      </div>
    )
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-background to-muted p-4">
      <Card className="w-full max-w-md shadow-lg">
        <CardHeader className="text-center pb-2">
          {/* Logo / Branding */}
          <div className="mx-auto w-16 h-16 bg-primary/10 rounded-full flex items-center justify-center mb-4">
            <Bot className="w-8 h-8 text-primary" />
          </div>
          <CardTitle className="text-2xl">Welcome Back</CardTitle>
          <CardDescription className="text-base">
            Sign in to continue to MCP Agent Builder
          </CardDescription>
        </CardHeader>

        <CardContent className="space-y-4">
          {/* Error Alert */}
          {error && (
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          {/* OAuth Providers */}
          {oauthProviders.length > 0 && (
            <div className="space-y-3">
              {oauthProviders.map((provider) => {
                const config = providerConfig[provider.name] || {
                  displayName: provider.name,
                  icon: <KeyRound className="w-5 h-5" />,
                  description: `Sign in with ${provider.name}`
                }
                return (
                  <Button
                    key={provider.name}
                    onClick={() => handleOAuthLogin(provider)}
                    className={`w-full h-12 text-base font-medium gap-3 ${config.buttonClass || ''}`}
                    disabled={isLoading}
                    variant={config.buttonClass ? undefined : 'outline'}
                  >
                    {isLoading ? (
                      <Loader2 className="w-5 h-5 animate-spin" />
                    ) : (
                      config.icon
                    )}
                    {isLoading ? 'Redirecting...' : config.displayName}
                  </Button>
                )
              })}
            </div>
          )}

          {/* Divider */}
          {oauthProviders.length > 0 && credentialsProviders.length > 0 && (
            <div className="relative">
              <div className="absolute inset-0 flex items-center">
                <span className="w-full border-t border-border" />
              </div>
              <div className="relative flex justify-center text-xs uppercase">
                <span className="bg-card px-2 text-muted-foreground">
                  or continue with
                </span>
              </div>
            </div>
          )}

          {/* Provider Selection (if multiple credentials providers) */}
          {credentialsProviders.length > 1 && !showCredentialsForm && (
            <div className="space-y-3">
              {credentialsProviders.map((provider) => {
                const config = providerConfig[provider.name] || {
                  displayName: provider.name,
                  icon: <KeyRound className="w-5 h-5" />,
                  description: `Sign in with ${provider.name}`
                }
                return (
                  <Button
                    key={provider.name}
                    onClick={() => setSelectedProvider(provider.name)}
                    className="w-full h-11"
                    variant="outline"
                  >
                    {config.icon}
                    {config.displayName}
                  </Button>
                )
              })}
            </div>
          )}

          {/* Credentials Form */}
          {(credentialsProviders.length === 1 || showCredentialsForm) && (
            <form className="space-y-4" onSubmit={handleSubmit}>
              {/* Back button if multiple providers */}
              {credentialsProviders.length > 1 && (
                <button
                  type="button"
                  onClick={() => setSelectedProvider(null)}
                  className="text-sm text-primary hover:underline inline-flex items-center gap-1"
                >
                  <span>&larr;</span> Back to provider selection
                </button>
              )}

              <div className="space-y-4">
                <div className="space-y-2">
                  <label htmlFor="username" className="text-sm font-medium text-foreground">
                    {selectedProvider === 'supabase' ? 'Email' : 'Username'}
                  </label>
                  <div className="relative">
                    <Mail className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                    <Input
                      id="username"
                      name="username"
                      type={selectedProvider === 'supabase' ? 'email' : 'text'}
                      required
                      value={username}
                      onChange={(e) => setUsername(e.target.value)}
                      className="pl-10 h-11"
                      placeholder={selectedProvider === 'supabase' ? 'you@example.com' : 'Enter your username'}
                    />
                  </div>
                </div>

                <div className="space-y-2">
                  <label htmlFor="password" className="text-sm font-medium text-foreground">
                    Password
                  </label>
                  <div className="relative">
                    <Lock className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                    <Input
                      id="password"
                      name="password"
                      type="password"
                      required
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      className="pl-10 h-11"
                      placeholder="Enter your password"
                    />
                  </div>
                </div>
              </div>

              <Button
                type="submit"
                className="w-full h-11 text-base font-medium"
                disabled={isLoading}
              >
                {isLoading ? (
                  <>
                    <Loader2 className="w-4 h-4 animate-spin" />
                    Signing in...
                  </>
                ) : (
                  'Sign in'
                )}
              </Button>
            </form>
          )}
        </CardContent>

        <CardFooter className="flex flex-col gap-2 text-center text-sm text-muted-foreground">
          <p>
            By signing in, you agree to our terms of service and privacy policy.
          </p>
        </CardFooter>
      </Card>
    </div>
  )
}
