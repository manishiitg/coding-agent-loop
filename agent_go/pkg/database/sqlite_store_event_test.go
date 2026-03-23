package database

import (
	"context"
	"testing"
	"time"

	"github.com/manishiitg/mcpagent/events"
)

func TestSQLiteStoreEventSnapshotsBufferedEvent(t *testing.T) {
	db := &SQLiteDB{
		eventBuffer:    make(map[string][]pendingEvent),
		batchSizeLimit: 10,
	}

	original := &events.AgentEvent{
		Type:      events.MCPServerConnectionStart,
		Timestamp: time.Now(),
		Data: &events.MCPServerConnectionEvent{
			ServerName: "workspace",
			Status:     "connected",
			ServerInfo: map[string]interface{}{"version": "1.0.0"},
		},
	}

	if err := db.StoreEvent(context.Background(), "session-1", original); err != nil {
		t.Fatalf("StoreEvent returned error: %v", err)
	}

	originalData := original.Data.(*events.MCPServerConnectionEvent)
	originalData.ServerInfo["version"] = "2.0.0"

	buffered := db.eventBuffer["session-1"][0].event
	if buffered == original {
		t.Fatal("expected buffered event to use a detached snapshot")
	}

	bufferedData, ok := buffered.Data.(*events.MCPServerConnectionEvent)
	if !ok {
		t.Fatalf("expected *MCPServerConnectionEvent, got %T", buffered.Data)
	}

	if bufferedData.ServerInfo["version"] != "1.0.0" {
		t.Fatalf("expected buffered server info to remain unchanged, got %v", bufferedData.ServerInfo["version"])
	}
}
