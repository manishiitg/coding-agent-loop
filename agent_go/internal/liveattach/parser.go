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
// This package is pure and has no side effects.
package liveattach

import "strings"

// MaxControlLineBytes caps a single control-mode line that the attach manager
// will buffer. A burst of pane output is emitted as one `%output` line, which
// can be large; 4 MiB matches the spike's scanner cap and sits far above any
// realistic single line while still bounding memory for a runaway producer.
const MaxControlLineBytes = 4 << 20 // 4 MiB

// MaxReplyBlockBytes caps the body a single %begin..%end block may accumulate.
// Without it, a torn or malformed guard line (a truncated %begin, a lost %end)
// puts Protocol into block mode permanently: every subsequent line — including
// all pane output — is swallowed into p.lines, so the pane silently freezes
// while memory grows without bound. Exceeding the cap instead aborts the block
// and surfaces an errored Reply, which fails the pending command and closes the
// stream, so the viewer reconnects and re-seeds.
//
// The largest legitimate block is the seed's history capture (bounded by the
// manager's backfill line budget at the pane width), which stays far below
// this; treat the cap as a corruption backstop, not a tuning knob.
const MaxReplyBlockBytes = 32 << 20 // 32 MiB

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
	// LineReplyBody is a line consumed into a pending %begin..%end command
	// reply by Protocol.Feed (including the %begin line itself). It carries no
	// pane bytes and no event; the completed reply is returned separately.
	LineReplyBody
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

// Reply is one completed in-band command reply: the block of lines tmux emits
// between `%begin <ts> <num> <flags>` and the matching `%end`/`%error`.
// Command replies are serialized with `%output` in the control stream, so a
// Reply is an exact barrier: pane output classified before it reflects state
// the command already saw, output after it does not.
type Reply struct {
	// Number is the command-number token from the %begin line. tmux assigns
	// numbers in submission order, so replies map FIFO onto sent commands.
	Number string
	// Lines are the verbatim body lines (trailing CR trimmed, escape bytes
	// intact — tmux does not octal-escape command output, unlike %output).
	Lines []string
	// Err is true when the block ended with %error instead of %end.
	Err bool
	// Overflow is true when the block was abandoned because it exceeded
	// MaxReplyBlockBytes (a torn/never-terminated guard). Err is always set
	// too; Overflow distinguishes protocol corruption from a tmux command that
	// legitimately failed, which callers may want to log differently.
	Overflow bool
}

// Protocol is a stateful control-mode line reader that frames command replies.
// Feed each scanner line in order; outside a reply block it behaves exactly
// like ClassifyLine, and %begin starts a block whose body lines are collected
// verbatim until the matching %end/%error (matched by command number, so body
// text that merely looks like a protocol line cannot terminate the block).
// Protocol is not safe for concurrent use; feed it from a single goroutine.
type Protocol struct {
	inBlock bool
	number  string
	lines   []string
	// bytes tracks the accumulated body size of the open block so a block that
	// never terminates cannot grow unbounded. See MaxReplyBlockBytes.
	bytes int
}

// resetBlock clears the open-block state and returns the collected lines.
func (p *Protocol) resetBlock() []string {
	lines := p.lines
	p.inBlock = false
	p.number = ""
	p.lines = nil
	p.bytes = 0
	return lines
}

// Feed consumes one raw control-mode line. It returns the line classification
// and, when this line completed a command reply, the finished Reply. Lines
// consumed into a block (including %begin/%end themselves) come back as
// LineReplyBody so callers can never route them into the pane stream.
func (p *Protocol) Feed(raw string) (ParsedLine, *Reply) {
	if p.inBlock {
		line := strings.TrimRight(raw, "\r")
		if kind, num := parseBlockGuard(line); num == p.number {
			switch kind {
			case "%end", "%error":
				number, err := p.number, kind == "%error"
				reply := &Reply{Number: number, Lines: p.resetBlock(), Err: err}
				return ParsedLine{Kind: LineReplyBody, Raw: line}, reply
			}
		}
		// Corruption backstop: abandon a block that outgrows MaxReplyBlockBytes
		// and report it as an error reply, rather than swallowing the rest of
		// the stream forever. Truncated is fine — the caller's recovery is to
		// fail the command and re-seed, not to salvage the body.
		if p.bytes+len(line) > MaxReplyBlockBytes {
			number := p.number
			p.resetBlock()
			reply := &Reply{Number: number, Err: true, Overflow: true}
			return ParsedLine{Kind: LineReplyBody, Raw: line}, reply
		}
		p.bytes += len(line)
		p.lines = append(p.lines, line)
		return ParsedLine{Kind: LineReplyBody, Raw: line}, nil
	}

	pl := ClassifyLine(raw)
	if pl.Kind == LineEvent && pl.Event == "%begin" {
		if kind, num := parseBlockGuard(pl.Raw); kind == "%begin" && num != "" {
			p.inBlock = true
			p.number = num
			p.lines = nil
			p.bytes = 0
			return ParsedLine{Kind: LineReplyBody, Raw: pl.Raw}, nil
		}
	}
	return pl, nil
}

// parseBlockGuard reads a "%begin/%end/%error <timestamp> <number> <flags>"
// guard line and returns its kind token and command number ("" when the line
// is not a well-formed guard).
func parseBlockGuard(line string) (string, string) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return "", ""
	}
	switch fields[0] {
	case "%begin", "%end", "%error":
		return fields[0], fields[2]
	}
	return "", ""
}
