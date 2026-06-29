// Package liveattach implements the tmux control-mode (`tmux -CC attach`)
// protocol parsing used by the live-attach terminal transport.
//
// The transport attaches one real (read-only) tmux control-mode client per
// selected terminal and streams the parsed pane bytes to a browser xterm over a
// WebSocket. This package is the pure, dependency-free core: it decodes the
// `%output` octal escaping and classifies each control-mode line so the manager
// can route pane bytes to the stream and notifications (`%layout-change`,
// `%window-renamed`, `%exit`, `%begin`/`%end`, …) to handlers — guaranteeing a
// control notification can never leak into the rendered pane byte stream.
//
// It is feature-flagged behind RUNLOOP_TERMINAL_LIVE_ATTACH at the call site;
// this package itself has no side effects.
package liveattach

import "strings"

// MaxControlLineBytes caps a single control-mode line that the attach manager
// will buffer. A burst of pane output is emitted as one `%output` line, which
// can be large; 4 MiB matches the spike's scanner cap and sits far above any
// realistic single line while still bounding memory for a runaway producer.
const MaxControlLineBytes = 4 << 20 // 4 MiB

// dcsControlModePrefix is the DCS string tmux writes once at the very start of
// the control-mode stream (ESC P 1000 p). It must be stripped before a line is
// classified, otherwise the first real line is misread.
const dcsControlModePrefix = "\x1bP1000p"

// LineKind classifies a single control-mode line.
type LineKind int

const (
	// LineStray is a non-empty line that is neither a `%output` line nor a
	// recognizable `%event` line (e.g. residue of the DCS string terminator).
	// Callers should ignore or log it; it must never reach the pane stream.
	LineStray LineKind = iota
	// LinePaneOutput is a "%output %<pane> <data>" line carrying pane bytes.
	LinePaneOutput
	// LineEvent is any other notification line beginning with '%'
	// (e.g. %layout-change, %window-renamed, %exit, %begin, %end,
	// %session-changed). Routed to handlers, never to the pane stream.
	LineEvent
	// LineEmpty is a blank line (after trimming). Callers ignore it.
	LineEmpty
)

// ParsedLine is the result of classifying one control-mode line.
type ParsedLine struct {
	Kind  LineKind
	Pane  string // pane id for LinePaneOutput, e.g. "%0"
	Data  []byte // decoded pane bytes for LinePaneOutput
	Event string // event name (first token) for LineEvent, e.g. "%exit"
	Raw   string // the trimmed line, with the DCS wrapper removed
}

// DecodeOutput converts tmux control-mode `%output` escaping into raw bytes.
// tmux escapes every byte it cannot emit literally — control characters,
// high-bit bytes, and the backslash itself — as a three-digit octal sequence
// "\ooo"; all other bytes pass through verbatim. A backslash that is not
// followed by exactly three octal digits is emitted literally (defensive: real
// tmux never produces this because it always escapes a literal backslash as
// \134, but the decoder must not corrupt or panic on malformed input).
func DecodeOutput(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c == '\\' && i+4 <= len(s) {
			d1, d2, d3 := s[i+1], s[i+2], s[i+3]
			if d1 >= '0' && d1 <= '7' && d2 >= '0' && d2 <= '7' && d3 >= '0' && d3 <= '7' {
				out = append(out, (d1-'0')<<6|(d2-'0')<<3|(d3-'0'))
				i += 4
				continue
			}
		}
		out = append(out, c)
		i++
	}
	return out
}

// ClassifyLine strips the DCS control-mode wrapper, trims a trailing CR, and
// classifies the line. It decodes pane bytes only for a genuine `%output` line,
// so an event notification can never be misrouted into the rendered stream.
func ClassifyLine(raw string) ParsedLine {
	line := strings.TrimPrefix(raw, dcsControlModePrefix)
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return ParsedLine{Kind: LineEmpty, Raw: line}
	}
	if strings.HasPrefix(line, "%output ") {
		rest := line[len("%output "):]
		// %output is "%output %<pane> <data>"; split off the pane id.
		sp := strings.IndexByte(rest, ' ')
		if sp < 0 {
			// Malformed: no data field. Treat as stray rather than risk emitting
			// the pane id token as pane bytes.
			return ParsedLine{Kind: LineStray, Raw: line}
		}
		return ParsedLine{
			Kind: LinePaneOutput,
			Pane: rest[:sp],
			Data: DecodeOutput(rest[sp+1:]),
			Raw:  line,
		}
	}
	if strings.HasPrefix(line, "%") {
		name := line
		if sp := strings.IndexByte(line, ' '); sp >= 0 {
			name = line[:sp]
		}
		return ParsedLine{Kind: LineEvent, Event: name, Raw: line}
	}
	return ParsedLine{Kind: LineStray, Raw: line}
}

// IsExit reports whether the line is the `%exit` notification, which signals the
// control client (and thus the attach) must stop. tmux emits `%exit` optionally
// followed by a reason; the Event field holds just the "%exit" token.
func (p ParsedLine) IsExit() bool {
	return p.Kind == LineEvent && p.Event == "%exit"
}
