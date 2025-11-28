package services

import (
	"regexp"
	"strings"
)

// Chunk represents a text chunk with metadata
type Chunk struct {
	Text      string `json:"text"`
	Index     int    `json:"index"`
	StartPos  int    `json:"start_pos"`
	EndPos    int    `json:"end_pos"`
	WordCount int    `json:"word_count"`
	CharCount int    `json:"char_count"`
}

// Chunker handles text chunking for embedding generation
type Chunker struct {
	chunkSize    int
	overlapSize  int
	minChunkSize int
}

// NewChunker creates a new chunker with default settings
func NewChunker() *Chunker {
	return &Chunker{
		chunkSize:    1000, // Default chunk size in characters
		overlapSize:  200,  // Default overlap size in characters
		minChunkSize: 100,  // Minimum chunk size
	}
}

// NewChunkerWithConfig creates a new chunker with custom settings
func NewChunkerWithConfig(chunkSize, overlapSize, minChunkSize int) *Chunker {
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	if overlapSize < 0 {
		overlapSize = 200
	}
	if minChunkSize <= 0 {
		minChunkSize = 100
	}
	if overlapSize >= chunkSize {
		overlapSize = chunkSize / 5 // Max 20% overlap
	}

	return &Chunker{
		chunkSize:    chunkSize,
		overlapSize:  overlapSize,
		minChunkSize: minChunkSize,
	}
}

// ChunkText chunks plain text into overlapping segments
func (c *Chunker) ChunkText(text string) []Chunk {
	if len(text) == 0 {
		return []Chunk{}
	}

	// Clean and normalize text
	text = c.cleanText(text)

	// If text is smaller than chunk size, return as single chunk
	if len(text) <= c.chunkSize {
		return []Chunk{
			{
				Text:      text,
				Index:     0,
				StartPos:  0,
				EndPos:    len(text),
				WordCount: c.countWords(text),
				CharCount: len(text),
			},
		}
	}

	var chunks []Chunk
	start := 0
	index := 0

	for start < len(text) {
		end := start + c.chunkSize

		// If this is not the last chunk, try to break at a sentence boundary
		if end < len(text) {
			end = c.findSentenceBoundary(text, start, end)
		}

		// If we couldn't find a good sentence boundary, break at word boundary
		if end == start+c.chunkSize && end < len(text) {
			end = c.findWordBoundary(text, start, end)
		}

		// Ensure end doesn't exceed text length
		if end > len(text) {
			end = len(text)
		}

		// Extract chunk text
		chunkText := text[start:end]

		// Skip chunks that are too small (except for the last chunk)
		if len(chunkText) < c.minChunkSize && end < len(text) {
			end = start + c.minChunkSize
			if end > len(text) {
				end = len(text)
			}
			chunkText = text[start:end]
		}

		chunks = append(chunks, Chunk{
			Text:      chunkText,
			Index:     index,
			StartPos:  start,
			EndPos:    end,
			WordCount: c.countWords(chunkText),
			CharCount: len(chunkText),
		})

		// Move start position with overlap
		start = end - c.overlapSize
		if start < 0 {
			start = 0
		}

		// Prevent infinite loop
		if start >= end {
			start = end
		}

		index++
	}

	return chunks
}

// ChunkMarkdown chunks markdown text with special handling for structure
func (c *Chunker) ChunkMarkdown(content string) []Chunk {
	if len(content) == 0 {
		return []Chunk{}
	}

	// Split by markdown headers first
	sections := c.splitByHeaders(content)

	var allChunks []Chunk
	index := 0

	for _, section := range sections {
		// Clean the section
		section = c.cleanText(section)

		if len(section) == 0 {
			continue
		}

		// If section is small enough, add as single chunk
		if len(section) <= c.chunkSize {
			allChunks = append(allChunks, Chunk{
				Text:      section,
				Index:     index,
				StartPos:  0,
				EndPos:    len(section),
				WordCount: c.countWords(section),
				CharCount: len(section),
			})
			index++
			continue
		}

		// Otherwise, chunk the section normally
		sectionChunks := c.ChunkText(section)
		for i := range sectionChunks {
			sectionChunks[i].Index = index
			index++
		}
		allChunks = append(allChunks, sectionChunks...)
	}

	return allChunks
}

// cleanText cleans and normalizes text
func (c *Chunker) cleanText(text string) string {
	// Remove excessive whitespace
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")

	// Remove leading/trailing whitespace
	text = strings.TrimSpace(text)

	// Remove excessive newlines
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")

	return text
}

// findSentenceBoundary finds a good sentence boundary within the chunk
func (c *Chunker) findSentenceBoundary(text string, start, end int) int {
	// Look for sentence endings within the last 200 characters
	searchStart := end - 200
	if searchStart < start {
		searchStart = start
	}

	searchText := text[searchStart:end]

	// Look for sentence endings (., !, ?) followed by whitespace
	sentenceEnd := regexp.MustCompile(`[.!?]\s+`)
	matches := sentenceEnd.FindAllStringIndex(searchText, -1)

	if len(matches) > 0 {
		// Use the last sentence ending
		lastMatch := matches[len(matches)-1]
		return searchStart + lastMatch[1]
	}

	return end
}

// findWordBoundary finds a word boundary within the chunk
func (c *Chunker) findWordBoundary(text string, start, end int) int {
	// Look for word boundaries within the last 100 characters
	searchStart := end - 100
	if searchStart < start {
		searchStart = start
	}

	searchText := text[searchStart:end]

	// Find the last whitespace character
	lastSpace := strings.LastIndex(searchText, " ")
	if lastSpace > 0 {
		return searchStart + lastSpace
	}

	return end
}

// splitByHeaders splits markdown content by headers
func (c *Chunker) splitByHeaders(content string) []string {
	// Split by markdown headers (# ## ### etc.)
	headerRegex := regexp.MustCompile(`(?m)^#{1,6}\s+.*$`)
	parts := headerRegex.Split(content, -1)

	var sections []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) > 0 {
			sections = append(sections, part)
		}
	}

	// If no headers found, return the whole content
	if len(sections) == 0 {
		return []string{content}
	}

	return sections
}

// countWords counts the number of words in text
func (c *Chunker) countWords(text string) int {
	words := strings.Fields(text)
	return len(words)
}

// GetChunkStats returns statistics about chunking
func (c *Chunker) GetChunkStats(chunks []Chunk) map[string]interface{} {
	if len(chunks) == 0 {
		return map[string]interface{}{
			"total_chunks":     0,
			"avg_chunk_size":   0,
			"total_characters": 0,
			"total_words":      0,
		}
	}

	totalChars := 0
	totalWords := 0
	minChars := len(chunks[0].Text)
	maxChars := len(chunks[0].Text)

	for _, chunk := range chunks {
		totalChars += chunk.CharCount
		totalWords += chunk.WordCount

		if chunk.CharCount < minChars {
			minChars = chunk.CharCount
		}
		if chunk.CharCount > maxChars {
			maxChars = chunk.CharCount
		}
	}

	return map[string]interface{}{
		"total_chunks":     len(chunks),
		"avg_chunk_size":   totalChars / len(chunks),
		"min_chunk_size":   minChars,
		"max_chunk_size":   maxChars,
		"total_characters": totalChars,
		"total_words":      totalWords,
		"chunk_size":       c.chunkSize,
		"overlap_size":     c.overlapSize,
	}
}

// SetChunkSize sets the chunk size
func (c *Chunker) SetChunkSize(size int) {
	if size > 0 {
		c.chunkSize = size
	}
}

// SetOverlapSize sets the overlap size
func (c *Chunker) SetOverlapSize(size int) {
	if size >= 0 && size < c.chunkSize {
		c.overlapSize = size
	}
}
