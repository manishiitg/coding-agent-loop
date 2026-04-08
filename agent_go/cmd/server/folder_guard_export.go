package server

import "context"

// ApplyChatModeFolderGuard wraps workspace tool executors with chat-mode folder guard.
// Exported for integration testing. See wrapExecutorsWithChatModeFolderGuard for details.
func ApplyChatModeFolderGuard(
	executors map[string]func(ctx context.Context, args map[string]interface{}) (string, error),
	readOnlyFolders []string,
	additionalWriteFolders ...string,
) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	return wrapExecutorsWithChatModeFolderGuard(executors, readOnlyFolders, additionalWriteFolders...)
}
