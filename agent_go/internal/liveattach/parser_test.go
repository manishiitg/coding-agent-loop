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
			name:      "exit",
			in:        "%exit",
			wantKind:  LineEvent, wantEvent: "%exit", wantExit: true,
		},
		{
			name:      "exit with reason",
			in:        "%exit server exited",
			wantKind:  LineEvent, wantEvent: "%exit", wantExit: true,
		},
		{
			name:      "layout change",
			in:        "%layout-change @0 b25d,80x24,0,0,0",
			wantKind:  LineEvent, wantEvent: "%layout-change",
		},
		{
			name:      "window renamed",
			in:        "%window-renamed @0 newname",
			wantKind:  LineEvent, wantEvent: "%window-renamed",
		},
		{
			name:      "session changed",
			in:        "%session-changed $0 main",
			wantKind:  LineEvent, wantEvent: "%session-changed",
		},
		{
			name:      "begin",
			in:        "%begin 1700000000 1 0",
			wantKind:  LineEvent, wantEvent: "%begin",
		},
		{
			name:      "end",
			in:        "%end 1700000000 1 0",
			wantKind:  LineEvent, wantEvent: "%end",
		},
		{
			name:      "bare percent event no args",
			in:        "%pause",
			wantKind:  LineEvent, wantEvent: "%pause",
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
