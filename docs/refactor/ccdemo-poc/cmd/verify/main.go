package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	c, _, err := websocket.DefaultDialer.Dial("ws://127.0.0.1:8742/ws", nil)
	if err != nil { fmt.Println("DIAL ERR", err); os.Exit(1) }
	defer c.Close()

	var total int
	var first string
	got := make(chan string, 64)
	go func() {
		for {
			_, b, err := c.ReadMessage()
			if err != nil { return }
			total += len(b)
			s := string(b)
			if first == "" { first = s }
			got <- s
		}
	}()

	// 1) backfill should arrive quickly
	time.Sleep(800 * time.Millisecond)
	fmt.Printf("BACKFILL_BYTES=%d  contains_pi=%v\n", len(first), strings.Contains(first, "pi v"))

	// 2) send a resize -> server resize-window
	c.WriteMessage(websocket.TextMessage, mustJSON(map[string]any{"type":"resize","cols":100,"rows":30}))
	time.Sleep(600 * time.Millisecond)

	// 3) send input: type "hello" then Enter (0x0d) as a binary frame
	c.WriteMessage(websocket.BinaryMessage, []byte("hello from browser via control-mode"))
	time.Sleep(1500 * time.Millisecond)

	// drain live bytes that arrived after input
	live := 0
	draining := true
	for draining {
		select {
		case <-got: live++
		default: draining = false
		}
	}
	fmt.Printf("LIVE_MESSAGES_AFTER_INPUT=%d  TOTAL_BYTES=%d\n", live, total)
}

func mustJSON(v any) []byte { b,_ := json.Marshal(v); return b }
