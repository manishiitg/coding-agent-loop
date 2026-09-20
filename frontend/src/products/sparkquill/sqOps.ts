// Pure helpers for the SparkQuill page bridge: the sandboxed worksheet iframe
// talks to the app only through `parent.postMessage({__sq: 1, op, ...})`, and
// the host turns answer/timer ops into the child's next tutor message. Kept
// free of React/DOM so it unit-tests directly; LearningApp wires these into
// its viewer message listener.

export const SQ_MAX_TEXT = 2000
export const SQ_MAX_ID = 32
export const SQ_MAX_TIMERS = 40
// 5 seconds to 6 hours: shorter is a misclick vector, longer is not a test.
export const SQ_MIN_TIMER_SECONDS = 5
export const SQ_MAX_TIMER_SECONDS = 21600

/** Question/test ids back to a safe token; '' when there is nothing usable. */
export function sanitizeSqId(raw: unknown): string {
  if (typeof raw !== 'string') return ''
  return raw.replace(/[^A-Za-z0-9_-]/g, '').slice(0, SQ_MAX_ID)
}

function capText(raw: unknown): string {
  if (typeof raw !== 'string') return ''
  return raw.slice(0, SQ_MAX_TEXT)
}

/**
 * A widget answer as the tutor should receive it: `Q3: 3/5`. The qid prefix
 * is what lets the tutor match the answer to its question; without one the
 * raw value goes through unchanged. Null when there is no answer to send.
 */
export function buildSqAnswerText(qid: unknown, value: unknown): string | null {
  const text = capText(value).trim()
  if (!text) return null
  const id = sanitizeSqId(qid)
  return id ? `${id}: ${text}` : text
}

/**
 * A timer firing as the tutor should receive it. Always prefixed: the tutor
 * prompt treats `[Timer]` lines as system events, never as her words — an
 * expiry must not read as something she said.
 */
export function buildSqTimerText(qid: unknown): string {
  const id = sanitizeSqId(qid)
  return id
    ? `[Timer] Time expired on ${id} with no answer.`
    : '[Timer] Time expired on this test.'
}

export interface SqTimerConfig {
  qid: string
  seconds: number
}

/**
 * Fallback theme for a generated page whose own `:root` variables are
 * missing or broken (seen live: guide prose pasted inside `<style>`
 * invalidated the whole `:root` rule, so every `var(--good)` fill went
 * transparent and the meters turned invisible). Injected FIRST so a page's
 * own valid variables always win; only fills the gap when they don't parse.
 */
export const SQ_FALLBACK_VARS = ':root{--bg:#fbf7ef;--ink:#16223a;--muted:#5b6b86;--sun:#f6b93b;--sun-soft:#fdeecb;--card:#ffffff;--line:#ece3d2;--good:#2f9e6f;--focus:#e08a3c}'

/**
 * Host-owned link bridge for a generated page shown in a sandboxed srcDoc
 * viewer. Two things the page cannot do safely on its own:
 *
 * - In-page tabs (`<a href="#s1">`): a srcDoc frame has no document URL of
 *   its own, so the click's default navigation unbinds srcDoc and blanks the
 *   page (Safari always; Chrome on re-render). Intercepted and scrolled to
 *   directly instead — no navigation, the page always survives.
 * - External links: these would navigate the frame AWAY from the report, and
 *   the sandbox blocks `target="_blank"` popups. Routed to the parent via an
 *   `open` op so they open in a real tab.
 */
export function withViewerLinkBridge(html: string): string {
  const style = `<style>${SQ_FALLBACK_VARS}</style>`
  const withVars = /<head[^>]*>/i.test(html)
    ? html.replace(/<head[^>]*>/i, (m) => m + style)
    : style + html
  return withVars + `
<script>(function(){
  document.addEventListener('click', function (e) {
    var el = e.target;
    var a = el && el.closest ? el.closest('a[href]') : null;
    if (!a) return;
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return;
    var href = a.getAttribute('href') || '';
    if (href.charAt(0) === '#') {
      // Same-page tab: scroll, never navigate — even a dead target must not
      // navigate, or the srcDoc page blanks itself.
      e.preventDefault();
      if (href.length > 1) {
        var t = document.getElementById(href.slice(1));
        if (t) t.scrollIntoView();
      } else {
        window.scrollTo(0, 0);
      }
      return;
    }
    if (/^https?:\\/\\//i.test(href)) {
      e.preventDefault();
      parent.postMessage({ __sq: 1, op: 'open', url: href }, '*');
    }
  });
})();</script>`
}

/** A page's timer-config post back to valid timers; drops junk entries. */
export function sanitizeSqTimerConfigs(raw: unknown): SqTimerConfig[] {
  if (!Array.isArray(raw)) return []
  const out: SqTimerConfig[] = []
  const seen = new Set<string>()
  for (const entry of raw.slice(0, SQ_MAX_TIMERS)) {
    if (!entry || typeof entry !== 'object') continue
    const { qid, seconds } = entry as { qid?: unknown; seconds?: unknown }
    const id = sanitizeSqId(qid)
    if (typeof seconds !== 'number' || !Number.isFinite(seconds)) continue
    const secs = Math.floor(seconds)
    if (secs < SQ_MIN_TIMER_SECONDS || secs > SQ_MAX_TIMER_SECONDS) continue
    if (seen.has(id)) continue
    seen.add(id)
    out.push({ qid: id, seconds: secs })
  }
  return out
}
