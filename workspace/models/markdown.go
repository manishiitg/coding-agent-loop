package models

// MarkdownStructure represents the analyzed structure of a markdown document
type MarkdownStructure struct {
	Headings   []Heading `json:"headings"`
	Tables     []Table   `json:"tables"`
	Lists      []List    `json:"lists"`
	CodeBlocks int       `json:"code_blocks"`
	Links      int       `json:"links"`
	Images     int       `json:"images"`
	Paragraphs int       `json:"paragraphs"`
}

// Heading represents a markdown heading
type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"line"`
}

// Table represents a markdown table
type Table struct {
	Index     int        `json:"index"`
	Headers   []string   `json:"headers"`
	Rows      int        `json:"rows"`
	Columns   int        `json:"columns"`
	LineStart int        `json:"line_start"`
	Data      [][]string `json:"data,omitempty"`
}

// List represents a markdown list
type List struct {
	Type      string   `json:"type"` // "unordered" or "ordered"
	Items     int      `json:"items"`
	LineStart int      `json:"line_start"`
	Content   []string `json:"content,omitempty"`
}

// PatchRequest represents the request to patch a document
type PatchRequest struct {
	TargetType     string `json:"target_type" binding:"required"`     // 'heading', 'table', 'list', 'paragraph', 'code_block'
	TargetSelector string `json:"target_selector" binding:"required"` // Selector (heading text, table index, list index, etc.)
	Operation      string `json:"operation" binding:"required"`       // 'append', 'prepend', 'replace', 'insert_after', 'insert_before'
	Content        string `json:"content" binding:"required"`         // New content to patch
}

// SearchRequest represents the request to search documents
type SearchRequest struct {
	Query  string `form:"query" binding:"required"`
	Folder string `form:"folder"` // Optional folder to search in
	Limit  int    `form:"limit,default=50"`
}

// SemanticSearchRequest represents the request for semantic search
type SemanticSearchRequest struct {
	Query  string `form:"query" binding:"required"`
	Folder string `form:"folder"`           // Optional folder to search in
	Limit  int    `form:"limit,default=10"` // Default to 10 for semantic search
}

// SemanticSearchResult represents a semantic search result
type SemanticSearchResult struct {
	FilePath     string  `json:"file_path"`
	ChunkText    string  `json:"chunk_text"`
	ChunkIndex   int     `json:"chunk_index"`
	Score        float64 `json:"score"`
	Folder       string  `json:"folder"`
	FileType     string  `json:"file_type"`
	WordCount    int     `json:"word_count"`
	CharCount    int     `json:"char_count"`
	SearchMethod string  `json:"search_method"` // "semantic" or "regex"
}

// SemanticSearchResponse represents a semantic search response
type SemanticSearchResponse struct {
	Query           string                 `json:"query"`
	SemanticResults []SemanticSearchResult `json:"semantic_results"`
	TotalResults    int                    `json:"total_results"`
	SearchMethod    string                 `json:"search_method"` // "semantic"
	ProcessingTime  float64                `json:"processing_time_ms"`
	EmbeddingModel  string                 `json:"embedding_model,omitempty"`
	VectorDBStatus  string                 `json:"vector_db_status"`
}

// FileProcessingRequest represents a request to process a file for embeddings
type FileProcessingRequest struct {
	FilePath string `json:"file_path" binding:"required"`
	Content  string `json:"content" binding:"required"`
	Action   string `json:"action" binding:"required"` // "create", "update", "delete"
}

// FileProcessingResponse represents the response from file processing
type FileProcessingResponse struct {
	FilePath            string                 `json:"file_path"`
	Action              string                 `json:"action"`
	ChunksCreated       int                    `json:"chunks_created"`
	EmbeddingsGenerated int                    `json:"embeddings_generated"`
	ProcessingTime      float64                `json:"processing_time_ms"`
	Success             bool                   `json:"success"`
	Error               string                 `json:"error,omitempty"`
	Stats               map[string]interface{} `json:"stats,omitempty"`
}

// NestedContentRequest represents the request to get nested content
type NestedContentRequest struct {
	Path string `form:"path"` // Path like "Introduction -> Getting Started" (optional, defaults to level 1 headings)
}
