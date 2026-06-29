// ccdemo: prove the live-attach terminal transport (CONTROL MODE) end-to-end
// against a REAL CLI (pi / codex) running in a fresh tmux test session.
//
// Pipeline: real CLI in tmux  →  `tmux -CC attach` (control mode, under a PTY)
//   →  parse %output (octal decode, strip DCS wrapper, route %events to a log)
//   →  WebSocket (binary pane bytes)  →  xterm.js + AttachAddon in a served page.
// Bidirectional: WS input → tmux send-keys -H ; WS resize → tmux resize-window.
//
// Reuses the control-mode %output parser from ../ptypoc/attach_cc_parse.go.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var (
	sessionFlag = flag.String("s", "ccdemo-live", "tmux session to attach")
	addrFlag    = flag.String("addr", "127.0.0.1:0", "listen addr (port 0 = pick free)")
	colsFlag    = flag.Int("cols", 120, "initial control-client cols")
	rowsFlag    = flag.Int("rows", 36, "initial control-client rows")
)

// ---- control-mode %output decode (from ../ptypoc/attach_cc_parse.go) ----

func decodeOutput(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c == '\\' && i+4 <= len(s) {
			d1, d2, d3 := s[i+1], s[i+2], s[i+3]
			if d1 >= '0' && d1 <= '7' && d2 >= '0' && d2 <= '7' && d3 >= '0' && d3 <= '7' {
				b := (d1-'0')<<6 | (d2-'0')<<3 | (d3 - '0')
				out = append(out, b)
				i += 4
				continue
			}
		}
		out = append(out, c)
		i++
	}
	return out
}

// ---- broadcast hub: decoded pane bytes → all connected WS viewers ----

type hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func newHub() *hub { return &hub{subs: map[chan []byte]struct{}{}} }

func (h *hub) sub() chan []byte {
	ch := make(chan []byte, 256)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) unsub(ch chan []byte) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *hub) broadcast(b []byte) {
	cp := make([]byte, len(b))
	copy(cp, b)
	h.mu.Lock()
	for ch := range h.subs {
		select {
		case ch <- cp:
		default: // slow viewer: drop (spike)
		}
	}
	h.mu.Unlock()
}

// ---- tmux helpers ----

func tmux(args ...string) error {
	cmd := exec.Command("tmux", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// sendInput forwards raw terminal input bytes to the session faithfully via
// send-keys -H (hex), so Enter (0d), Ctrl-C (03), arrows, paste all pass through.
func sendInput(session string, data []byte) {
	if len(data) == 0 {
		return
	}
	args := []string{"send-keys", "-t", session, "-H"}
	for _, b := range data {
		args = append(args, fmt.Sprintf("%02x", b))
	}
	if err := tmux(args...); err != nil {
		log.Printf("[input] send-keys err: %v", err)
	}
}

// resize uses resize-window — works because the session is window-size latest.
func resize(session string, cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	if err := tmux("resize-window", "-t", session, "-x", fmt.Sprint(cols), "-y", fmt.Sprint(rows)); err != nil {
		log.Printf("[resize] resize-window err: %v", err)
	}
}

// backfill: one capture-pane snapshot (with color escapes + scrollback) so a
// fresh viewer sees current state before the live stream resumes.
func backfill(session string) []byte {
	out, err := exec.Command("tmux", "capture-pane", "-t", session, "-p", "-e", "-S", "-1000").Output()
	if err != nil {
		log.Printf("[backfill] capture-pane err: %v", err)
		return nil
	}
	// clear xterm, home cursor, then write captured lines (\n -> \r\n).
	body := strings.ReplaceAll(string(out), "\n", "\r\n")
	return append([]byte("\x1b[H\x1b[2J"), []byte(body)...)
}

// ---- control-mode attach reader ----

func attachControlMode(ctx context.Context, session string, h *hub, evlog *os.File) error {
	cmd := exec.CommandContext(ctx, "tmux", "-CC", "attach", "-t", session)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	// Control mode still needs a PTY for the client's own stdio (pipes fail tcgetattr).
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(*colsFlag), Rows: uint16(*rowsFlag)})
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		ptmx.Close()
		_ = cmd.Process.Kill()
	}()

	sc := bufio.NewScanner(ptmx)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		line = strings.TrimPrefix(line, "\x1bP1000p") // strip the DCS control-mode wrapper
		switch {
		case strings.HasPrefix(line, "%output "):
			rest := line[len("%output "):]
			sp := strings.IndexByte(rest, ' ')
			if sp < 0 {
				continue
			}
			h.broadcast(decodeOutput(rest[sp+1:]))
		case strings.HasPrefix(line, "%exit"):
			fmt.Fprintf(evlog, "%s  EVENT: %s\n", time.Now().Format("15:04:05"), line)
			return fmt.Errorf("control client exited: %s", line)
		case strings.HasPrefix(line, "%"):
			// %layout-change / %window-renamed / %window-pane-changed / %begin/%end ...
			fmt.Fprintf(evlog, "%s  EVENT: %s\n", time.Now().Format("15:04:05"), line)
		default:
			if strings.TrimSpace(line) != "" {
				fmt.Fprintf(evlog, "%s  STRAY: %q\n", time.Now().Format("15:04:05"), line)
			}
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(evlog, "SCANNER-ERR: %v\n", err)
		return err
	}
	return nil
}

// ---- WebSocket viewer ----

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func (s *server) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()
	log.Printf("[ws] viewer connected from %s", r.RemoteAddr)

	// 1) backfill snapshot, then live stream.
	if bf := backfill(s.session); len(bf) > 0 {
		_ = c.WriteMessage(websocket.BinaryMessage, bf)
	}
	ch := s.hub.sub()
	defer s.hub.unsub(ch)

	// writer: hub bytes -> ws (binary)
	writeMu := sync.Mutex{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for b := range ch {
			writeMu.Lock()
			err := c.WriteMessage(websocket.BinaryMessage, b)
			writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}()

	// reader: ws -> tmux. binary frame = raw input ; text frame = JSON control.
	for {
		mt, data, err := c.ReadMessage()
		if err != nil {
			break
		}
		switch mt {
		case websocket.BinaryMessage:
			sendInput(s.session, data)
		case websocket.TextMessage:
			var ctrl struct {
				Type       string `json:"type"`
				Cols, Rows int
			}
			if json.Unmarshal(data, &ctrl) == nil && ctrl.Type == "resize" {
				resize(s.session, ctrl.Cols, ctrl.Rows)
			} else {
				// treat any other text as input too (robustness)
				sendInput(s.session, data)
			}
		}
	}
	log.Printf("[ws] viewer disconnected")
	select {
	case <-done:
	default:
	}
}

type server struct {
	session string
	hub     *hub
}

func main() {
	flag.Parse()
	h := newHub()
	srv := &server{session: *sessionFlag, hub: h}

	evlog, err := os.Create("events.log")
	if err != nil {
		log.Fatal(err)
	}
	defer evlog.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		if err := attachControlMode(ctx, *sessionFlag, h, evlog); err != nil {
			log.Printf("[attach] ended: %v", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", srv.handleWS)
	mux.Handle("/", http.FileServer(http.Dir("static")))

	ln, err := newListener(*addrFlag)
	if err != nil {
		log.Fatal(err)
	}
	url := fmt.Sprintf("http://%s/", ln.Addr().String())
	fmt.Printf("\n==================================================\n")
	fmt.Printf("  ccdemo live-attach (CONTROL MODE) is RUNNING\n")
	fmt.Printf("  session : %s\n", *sessionFlag)
	fmt.Printf("  OPEN    : %s\n", url)
	fmt.Printf("==================================================\n\n")
	log.Fatal(http.Serve(ln, mux))
}
