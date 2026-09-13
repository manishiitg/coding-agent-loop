package server

// Remove only a trailing replay of a complete exchange whose occurrences are
// already accounted for in the native transcript and earlier persisted history.
// Single unanswered user messages, structured tool entries, and genuine repeated
// native turns remain untouched. This repairs old saves from an out-of-date Go
// agent after retained CLI turns advanced the same conversation.
func trimNativeTranscriptStaleTail(persisted, native []builderConversationMessage) int {
	end := len(persisted)
	for end >= 4 {
		user, answer := persisted[end-2], persisted[end-1]
		if user.Role != "human" || answer.Role != "ai" || len(user.Parts) != 1 || len(answer.Parts) != 1 || user.Parts[0].Text == "" || answer.Parts[0].Text == "" {
			break
		}
		userKey, answerKey := builderConversationMessageKey(user), builderConversationMessageKey(answer)
		countPairs := func(messages []builderConversationMessage) (count, last int) {
			last = -1
			for i := 0; i+1 < len(messages); i++ {
				if builderConversationMessageKey(messages[i]) == userKey && builderConversationMessageKey(messages[i+1]) == answerKey {
					count++
					last = i + 1
				}
			}
			return
		}
		nativeCount, nativeLast := countPairs(native)
		persistedCount, _ := countPairs(persisted[:end])
		if nativeCount == 0 || persistedCount <= nativeCount || nativeLast == len(native)-1 {
			break
		}
		// Require a newer native message already present before the replay.
		newer := make(map[string]bool)
		for _, message := range native[nativeLast+1:] {
			newer[builderConversationMessageKey(message)] = true
		}
		foundNewer := false
		_, previousPair := countPairs(persisted[:end-2])
		for _, message := range persisted[previousPair+1 : end-2] {
			if newer[builderConversationMessageKey(message)] {
				foundNewer = true
				break
			}
		}
		if !foundNewer {
			break
		}
		end -= 2
	}
	return end
}
