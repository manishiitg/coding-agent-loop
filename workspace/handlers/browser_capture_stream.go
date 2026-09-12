package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Bundled user-browser capture consumes the same selected-tab stream as the
// viewer. Native `record start` pins a CDP target and misses subsequent tabs.
// Entries are owned by browserCaptureLock. A service restart deliberately makes
// a persisted recording inactive rather than pretending its encoder survived.
var captureStreams = map[string]*captureStream{}

type captureStream struct {
	stop     chan struct{}
	done     chan struct{}
	once     sync.Once
	cancel   context.CancelFunc
	frames   int
	nonBlank bool
	err      error
}

func startCaptureStream(port int, output string) (*captureStream, error) {
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: 5 * time.Second}).Dial(
		fmt.Sprintf("ws://127.0.0.1:%d/", port), http.Header{"Origin": []string{"http://localhost"}})
	if err != nil {
		return nil, fmt.Errorf("connect recording stream: %w", err)
	}
	conn.SetReadLimit(8 << 20)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-loglevel", "error", "-f", "image2pipe", "-c:v", "mjpeg", "-framerate", "10", "-i", "pipe:0", "-vf", "scale=1280:720:force_original_aspect_ratio=decrease,pad=1280:720:(ow-iw)/2:(oh-ih)/2", "-c:v", "libvpx", "-deadline", "realtime", "-b:v", "1M", "-threads", "1", output)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		conn.Close()
		cancel()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		stdin.Close()
		conn.Close()
		cancel()
		return nil, err
	}
	stream := &captureStream{stop: make(chan struct{}), done: make(chan struct{}), cancel: cancel}
	incoming := make(chan []byte, 1)
	disconnected := make(chan error, 1)
	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				disconnected <- err
				return
			}
			var message struct {
				Type string `json:"type"`
				Data string `json:"data"`
			}
			if json.Unmarshal(raw, &message) != nil || message.Type != "frame" {
				continue
			}
			frame, err := base64.StdEncoding.DecodeString(message.Data)
			if err != nil || len(frame) == 0 {
				continue
			}
			// Keep the latest frame, including updates caused by selecting a new tab.
			select {
			case <-incoming:
			default:
			}
			select {
			case incoming <- frame:
			default:
			}
		}
	}()
	ready := make(chan struct{})
	go func() {
		defer close(stream.done)
		defer cancel()
		defer conn.Close()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		var latest []byte
		var readyOnce sync.Once
		running := true
		for running {
			select {
			case <-stream.stop:
				running = false
			case err := <-disconnected:
				stream.err = fmt.Errorf("browser stream disconnected: %w", err)
				running = false
			case latest = <-incoming:
				if !stream.nonBlank {
					stream.nonBlank = captureFrameHasContent(latest)
				}
			case <-ticker.C:
				if len(latest) == 0 {
					continue
				}
				if _, err := stdin.Write(latest); err != nil {
					stream.err = err
					running = false
					continue
				}
				stream.frames++
				readyOnce.Do(func() { close(ready) })
			}
		}
		stdin.Close()
		// Bound encoder finalization even if ffmpeg stalls.
		timer := time.AfterFunc(10*time.Second, cancel)
		if err := cmd.Wait(); err != nil && stream.err == nil {
			stream.err = fmt.Errorf("video encoder: %w", err)
		}
		timer.Stop()
	}()
	select {
	case <-ready:
		return stream, nil
	case <-stream.done:
		return nil, fmt.Errorf("recording ended before its first frame: %v", stream.err)
	case <-time.After(10 * time.Second):
		stream.finish()
		return nil, fmt.Errorf("recording stream produced no frames")
	}
}

func (s *captureStream) active() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}
func (s *captureStream) finish() {
	s.once.Do(func() { close(s.stop) })
	select {
	case <-s.done:
	case <-time.After(12 * time.Second):
		s.cancel()
		<-s.done
	}
}

// A valid container/byte count does not establish that a page was recorded.
// Detect uniform blank frames; semantic correctness still needs visual review.
func captureFrameHasContent(frame []byte) bool {
	img, err := jpeg.Decode(bytes.NewReader(frame))
	if err != nil {
		return false
	}
	bounds := img.Bounds()
	if bounds.Dx() < 2 || bounds.Dy() < 2 {
		return false
	}
	var min, max uint32 = 65535, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y += maxInt(1, bounds.Dy()/32) {
		for x := bounds.Min.X; x < bounds.Max.X; x += maxInt(1, bounds.Dx()/32) {
			r, g, b, _ := img.At(x, y).RGBA()
			value := (r + g + b) / 3
			if value < min {
				min = value
			}
			if value > max {
				max = value
			}
		}
	}
	return max-min > 4096
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Check the finalized file can actually be decoded, not only that it exists.
func validateCaptureVideo(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-xerror", "-i", path, "-frames:v", "1", "-f", "null", "-")
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("recorded video could not be decoded: %w", err)
	}
	return nil
}
