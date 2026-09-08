// Whether finishing a voice recording sends it right away instead of just
// landing the transcript in the composer for the user to review and send —
// SparkQuill only (see MicButton's own doc comment: AgentWorks deliberately
// never auto-sends). Off by default; a parent opts in from Settings. A
// child talking to Quill by voice is the common case this exists for.

const VOICE_AUTO_SEND_KEY = 'sq-child-voice-autosend'

export function readVoiceAutoSendPref(): boolean {
  try {
    return localStorage.getItem(VOICE_AUTO_SEND_KEY) === '1'
  } catch {
    return false
  }
}

export function persistVoiceAutoSendPref(on: boolean) {
  try {
    localStorage.setItem(VOICE_AUTO_SEND_KEY, on ? '1' : '0')
  } catch {
    // ignore — worst case the preference doesn't survive a reload
  }
}
