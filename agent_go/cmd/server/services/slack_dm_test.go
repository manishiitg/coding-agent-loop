package services

import "testing"

// A 1:1 DM is one chat keyed by its channel: replies post directly. Channel
// threads (and replies inside a DM thread) stay threaded.
func TestSlackThreadOptionPostsDMRepliesDirectly(t *testing.T) {
	if got := slackThreadOption("D0DMCHAN01", "D0DMCHAN01"); len(got) != 0 {
		t.Fatalf("a whole-DM reply was threaded: %d options", len(got))
	}
	if got := slackThreadOption("C0CHAN", ""); len(got) != 0 {
		t.Fatalf("a post with no thread was threaded: %d options", len(got))
	}
	if got := slackThreadOption("C0CHAN", "1790000000.000100"); len(got) != 1 {
		t.Fatalf("a channel thread reply lost its thread: %d options", len(got))
	}
	if !threadIsWholeChat(ThreadID{Platform: "slack", ChannelID: "D0DMCHAN01", ThreadTS: "D0DMCHAN01"}) ||
		threadIsWholeChat(ThreadID{Platform: "slack", ChannelID: "C0CHAN", ThreadTS: "1790000000.000100"}) {
		t.Fatal("whole-chat detection is wrong")
	}
}
