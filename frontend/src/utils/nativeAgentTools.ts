/**
 * "Native agent tools" is on by default for every workflow and crew: an unset
 * capabilities.native_agent_tools means on, and only an explicit false turns
 * it off. Mirrors nativeAgentToolsEnabled in the server.
 */
export function nativeAgentToolsEnabled(setting: unknown): boolean {
  return setting !== false
}
