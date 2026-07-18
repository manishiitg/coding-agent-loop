package liveattach

import (
	"bytes"
	"strings"
	"testing"
)

func TestDecodeOutput(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []byte
	}{
		{"plain printable", "hello world", []byte("hello world")},
		{"empty", "", []byte("")},
		{"esc octal 033", `\033`, []byte{0x1b}},
		{"cr octal 015", `\015`, []byte{0x0d}},
		{"lf octal 012", `\012`, []byte{0x0a}},
		{"backslash octal 134", `\134`, []byte{0x5c}},
		{"null octal 000", `\000`, []byte{0x00}},
		{"del octal 177", `\177`, []byte{0x7f}},
		{"high byte octal 357", `\357`, []byte{0xef}},
		{"esc seq sgr", `a\033[1mB`, append(append([]byte("a"), 0x1b), []byte("[1mB")...)},
		{"osc title with bel", `\033]0;title\007`, append(append(append([]byte{0x1b}, []byte("]0;title")...), 0x07), nil...)},
		{"crlf in data", `line1\015\012line2`, []byte("line1\r\nline2")},
		{"two octals adjacent", `\033\033`, []byte{0x1b, 0x1b}},
		// Defensive / malformed: a backslash not followed by 3 octal digits is
		// emitted literally (real tmux never produces this).
		{"backslash then nonoctal", `x\9y`, []byte(`x\9y`)},
		{"backslash digit 8 nonoctal", `\080`, []byte(`\080`)},
		{"trailing lone backslash", `ab\`, []byte(`ab\`)},
		{"trailing short escape", `ab\12`, []byte(`ab\12`)},
		{"literal backslash mid mixed with escape", `a\134\033b`, []byte{'a', 0x5c, 0x1b, 'b'}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecodeOutput(tc.in)
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("DecodeOutput(%q) = %v (%q), want %v (%q)", tc.in, got, got, tc.want, tc.want)
			}
		})
	}
}

func TestDecodeOutputLargeLine(t *testing.T) {
	// A large %output payload (well under the 4 MiB cap) must decode fully.
	const reps = 200000
	var sb strings.Builder
	for i := 0; i < reps; i++ {
		sb.WriteString(`\033`)
	}
	got := DecodeOutput(sb.String())
	if len(got) != reps {
		t.Fatalf("decoded length = %d, want %d", len(got), reps)
	}
	for i, b := range got {
		if b != 0x1b {
			t.Fatalf("byte %d = %#x, want 0x1b", i, b)
		}
	}
}

func TestMaxControlLineBytes(t *testing.T) {
	if MaxControlLineBytes != 4<<20 {
		t.Fatalf("MaxControlLineBytes = %d, want %d", MaxControlLineBytes, 4<<20)
	}
}

func TestClassifyLine(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantKind  LineKind
		wantPane  string
		wantData  []byte
		wantEvent string
		wantExit  bool
	}{
		{
			name:     "output simple",
			in:       "%output %1 hello",
			wantKind: LinePaneOutput, wantPane: "%1", wantData: []byte("hello"),
		},
		{
			name:     "output with octal escape",
			in:       `%output %0 hi\033[0m`,
			wantKind: LinePaneOutput, wantPane: "%0",
			wantData: append(append([]byte("hi"), 0x1b), []byte("[0m")...),
		},
		{
			name:     "output multiline crlf",
			in:       `%output %2 a\015\012b`,
			wantKind: LinePaneOutput, wantPane: "%2", wantData: []byte("a\r\nb"),
		},
		{
			name:     "output dcs prefix stripped",
			in:       "\x1bP1000p%output %0 hi",
			wantKind: LinePaneOutput, wantPane: "%0", wantData: []byte("hi"),
		},
		{
			name:     "output trailing cr trimmed",
			in:       "%output %0 hi\r",
			wantKind: LinePaneOutput, wantPane: "%0", wantData: []byte("hi"),
		},
		{
			name:     "output empty data field",
			in:       "%output %0 ",
			wantKind: LinePaneOutput, wantPane: "%0", wantData: []byte(""),
		},
		{
			name:     "output no data field is stray",
			in:       "%output %0",
			wantKind: LineStray,
		},
		{
			name:     "exit",
			in:       "%exit",
			wantKind: LineEvent, wantEvent: "%exit", wantExit: true,
		},
		{
			name:     "exit with reason",
			in:       "%exit server exited",
			wantKind: LineEvent, wantEvent: "%exit", wantExit: true,
		},
		{
			name:     "layout change",
			in:       "%layout-change @0 b25d,80x24,0,0,0",
			wantKind: LineEvent, wantEvent: "%layout-change",
		},
		{
			name:     "window renamed",
			in:       "%window-renamed @0 newname",
			wantKind: LineEvent, wantEvent: "%window-renamed",
		},
		{
			name:     "session changed",
			in:       "%session-changed $0 main",
			wantKind: LineEvent, wantEvent: "%session-changed",
		},
		{
			name:     "begin",
			in:       "%begin 1700000000 1 0",
			wantKind: LineEvent, wantEvent: "%begin",
		},
		{
			name:     "end",
			in:       "%end 1700000000 1 0",
			wantKind: LineEvent, wantEvent: "%end",
		},
		{
			name:     "bare percent event no args",
			in:       "%pause",
			wantKind: LineEvent, wantEvent: "%pause",
		},
		{
			name:     "empty line",
			in:       "",
			wantKind: LineEmpty,
		},
		{
			name:     "whitespace only line",
			in:       "   ",
			wantKind: LineEmpty,
		},
		{
			name:     "stray garbage",
			in:       "garbage not a protocol line",
			wantKind: LineStray,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyLine(tc.in)
			if got.Kind != tc.wantKind {
				t.Fatalf("Kind = %v, want %v (line=%q)", got.Kind, tc.wantKind, tc.in)
			}
			if got.Pane != tc.wantPane {
				t.Fatalf("Pane = %q, want %q", got.Pane, tc.wantPane)
			}
			if tc.wantKind == LinePaneOutput && !bytes.Equal(got.Data, tc.wantData) {
				t.Fatalf("Data = %q, want %q", got.Data, tc.wantData)
			}
			if got.Event != tc.wantEvent {
				t.Fatalf("Event = %q, want %q", got.Event, tc.wantEvent)
			}
			if got.IsExit() != tc.wantExit {
				t.Fatalf("IsExit() = %v, want %v", got.IsExit(), tc.wantExit)
			}
			// An event line must never carry decoded pane bytes.
			if tc.wantKind == LineEvent && len(got.Data) != 0 {
				t.Fatalf("event line leaked pane Data = %q", got.Data)
			}
		})
	}
}

func TestProtocolFramesReplyBlocks(t *testing.T) {
	p := &Protocol{}

	feed := func(line string) (ParsedLine, *Reply) {
		t.Helper()
		return p.Feed(line)
	}

	// Output outside any block is classified normally.
	pl, reply := feed("%output %1 before")
	if pl.Kind != LinePaneOutput || string(pl.Data) != "before" || reply != nil {
		t.Fatalf("pre-block output = %+v reply=%v", pl, reply)
	}

	// %begin opens a block; the guard line itself is reply body.
	pl, reply = feed("%begin 1700000000 42 1")
	if pl.Kind != LineReplyBody || reply != nil {
		t.Fatalf("begin line = %+v reply=%v", pl, reply)
	}

	// Body lines are verbatim — even ones that look like protocol lines, and
	// ones carrying raw escape bytes.
	bodies := []string{
		"\x1b[31mRED\x1b[39m line",
		"%output %1 this is screen text, not pane output",
		"%end 1700000000 999 1", // wrong number: still body
		"",
	}
	for _, b := range bodies {
		pl, reply = feed(b)
		if pl.Kind != LineReplyBody || reply != nil {
			t.Fatalf("body line %q = %+v reply=%v", b, pl, reply)
		}
	}

	// Matching %end completes the reply with all body lines intact.
	pl, reply = feed("%end 1700000001 42 1")
	if pl.Kind != LineReplyBody || reply == nil {
		t.Fatalf("end line = %+v reply=%v", pl, reply)
	}
	if reply.Err || reply.Number != "42" {
		t.Fatalf("reply = %+v", reply)
	}
	if len(reply.Lines) != len(bodies) {
		t.Fatalf("reply lines = %q, want %d lines", reply.Lines, len(bodies))
	}
	for i, want := range bodies {
		if reply.Lines[i] != want {
			t.Fatalf("reply line %d = %q, want %q", i, reply.Lines[i], want)
		}
	}

	// Output after the block flows normally again.
	pl, reply = feed("%output %1 after")
	if pl.Kind != LinePaneOutput || string(pl.Data) != "after" || reply != nil {
		t.Fatalf("post-block output = %+v reply=%v", pl, reply)
	}
}

func TestProtocolErrorBlock(t *testing.T) {
	p := &Protocol{}
	p.Feed("%begin 1700000000 7 1")
	p.Feed("parse error: unknown command")
	_, reply := p.Feed("%error 1700000000 7 1")
	if reply == nil || !reply.Err || reply.Number != "7" {
		t.Fatalf("error reply = %+v", reply)
	}
	if len(reply.Lines) != 1 || reply.Lines[0] != "parse error: unknown command" {
		t.Fatalf("error reply lines = %q", reply.Lines)
	}
	// The protocol resumes normal classification after an %error block.
	pl, _ := p.Feed("%output %1 ok")
	if pl.Kind != LinePaneOutput {
		t.Fatalf("post-error line kind = %v", pl.Kind)
	}
}

func TestProtocolEmptyReplyAndCRTrim(t *testing.T) {
	p := &Protocol{}
	// The initial attach handshake is an empty %begin/%end pair; lines may
	// carry a trailing CR from the control client's PTY.
	p.Feed("\x1bP1000p%begin 1700000000 3 0\r")
	_, reply := p.Feed("%end 1700000000 3 0\r")
	if reply == nil || reply.Err || reply.Number != "3" || len(reply.Lines) != 0 {
		t.Fatalf("attach handshake reply = %+v", reply)
	}
}

func TestProtocolAbandonsUnterminatedBlock(t *testing.T) {
	p := &Protocol{}
	p.Feed("%begin 1700000000 9 1")

	// A block whose %end never arrives (torn/malformed guard) must not swallow
	// the stream forever: without the cap, every line below — including pane
	// output — is absorbed into the open block and the pane silently freezes.
	line := strings.Repeat("x", 1<<20)
	var overflow *Reply
	for i := 0; i < (MaxReplyBlockBytes/len(line))+2; i++ {
		if _, reply := p.Feed(line); reply != nil {
			overflow = reply
			break
		}
	}
	if overflow == nil {
		t.Fatal("unterminated block was never abandoned")
	}
	if !overflow.Err || !overflow.Overflow || overflow.Number != "9" {
		t.Fatalf("overflow reply = %+v, want errored overflow for command 9", overflow)
	}

	// Having abandoned the block, the protocol resumes normal classification so
	// the transport recovers by failing the command and re-seeding.
	pl, _ := p.Feed("%output %1 back-to-normal")
	if pl.Kind != LinePaneOutput || string(pl.Data) != "back-to-normal" {
		t.Fatalf("post-overflow line = %+v, want pane output", pl)
	}
}

func TestProtocolBlockByteBudgetResetsPerBlock(t *testing.T) {
	p := &Protocol{}
	half := strings.Repeat("y", MaxReplyBlockBytes/2)

	// Two consecutive blocks that are each individually under the cap must both
	// complete: the budget is per-block, not per-Protocol-lifetime.
	for _, number := range []string{"1", "2"} {
		p.Feed("%begin 1700000000 " + number + " 1")
		if _, reply := p.Feed(half); reply != nil {
			t.Fatalf("block %s overflowed on a single under-cap line", number)
		}
		_, reply := p.Feed("%end 1700000000 " + number + " 1")
		if reply == nil || reply.Err || reply.Overflow || reply.Number != number {
			t.Fatalf("block %s reply = %+v, want clean completion", number, reply)
		}
	}
}
