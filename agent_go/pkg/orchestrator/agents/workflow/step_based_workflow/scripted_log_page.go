package step_based_workflow

import "fmt"

// Paginate by Unicode characters so output is never silently cut or split
// in the middle of a UTF-8 sequence. The full log remains on disk.
func scriptedLogPage(content string, offset int) string {
	chars := []rune(content)
	if offset < 0 {
		offset = 0
	}
	if offset > len(chars) {
		offset = len(chars)
	}
	end := offset + 12000
	if end > len(chars) {
		end = len(chars)
	}
	page := fmt.Sprintf("Characters %d–%d of %d\n%s\n", offset, end, len(chars), string(chars[offset:end]))
	if end < len(chars) {
		page += fmt.Sprintf("More output available: call debug_step with the same step/group/iteration and log_offset=%d (next_log_offset).\n", end)
	}
	return page
}
