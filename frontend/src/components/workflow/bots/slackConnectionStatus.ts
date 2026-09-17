export function slackConnectionStatus(enabled: boolean, botMode: boolean, loading: boolean, testing: boolean, test: { success: boolean } | null) {
  if (loading) return 'Loading…'
  if (!enabled || !botMode) return 'Not connected'
  if (testing) return 'Testing…'
  if (test) return test.success ? 'Connected' : 'Test failed'
  return 'Configured · not tested'
}
