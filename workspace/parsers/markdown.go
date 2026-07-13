package parsers

import (
	"regexp"
	"strings"
	"github.com/manishiitg/coding-agent-loop/workspace/models"
)

// ParseMarkdown parses markdown content and returns its structure
func ParseMarkdown(content string) *models.MarkdownStructure {
	lines := strings.Split(content, "\n")

	structure := &models.MarkdownStructure{
		Headings:   []models.Heading{},
		Tables:     []models.Table{},
		Lists:      []models.List{},
		CodeBlocks: 0,
		Links:      0,
		Images:     0,
		Paragraphs: 0,
	}

	// Parse headings
	headingRegex := regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	for i, line := range lines {
		if matches := headingRegex.FindStringSubmatch(line); matches != nil {
			level := len(matches[1])
			text := strings.TrimSpace(matches[2])
			structure.Headings = append(structure.Headings, models.Heading{
				Level: level,
				Text:  text,
				Line:  i + 1,
			})
		}
	}

	// Parse tables
	structure.Tables = parseTables(lines)

	// Parse lists
	structure.Lists = parseLists(lines)

	// Count other elements
	structure.CodeBlocks = countCodeBlocks(lines)
	structure.Links = countLinks(content)
	structure.Images = countImages(content)
	structure.Paragraphs = countParagraphs(lines)

	return structure
}

// parseTables extracts table information from markdown lines
func parseTables(lines []string) []models.Table {
	var tables []models.Table
	tableIndex := 0
	inTable := false
	var headers []string
	var data [][]string
	lineStart := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if line is a table row (contains |)
		if strings.Contains(trimmed, "|") && !strings.HasPrefix(trimmed, "```") {
			if !inTable {
				// Start of new table
				inTable = true
				lineStart = i + 1
				headers = parseTableRow(trimmed)
				data = [][]string{}
			} else {
				// Check if it's a separator line (contains only |, -, and spaces)
				if isTableSeparator(trimmed) {
					continue
				}
				// It's a data row
				row := parseTableRow(trimmed)
				if len(row) > 0 {
					data = append(data, row)
				}
			}
		} else if inTable {
			// End of table
			if len(headers) > 0 {
				tables = append(tables, models.Table{
					Index:     tableIndex,
					Headers:   headers,
					Rows:      len(data),
					Columns:   len(headers),
					LineStart: lineStart,
					Data:      data,
				})
				tableIndex++
			}
			inTable = false
			headers = []string{}
			data = [][]string{}
		}
	}

	// Handle table at end of document
	if inTable && len(headers) > 0 {
		tables = append(tables, models.Table{
			Index:     tableIndex,
			Headers:   headers,
			Rows:      len(data),
			Columns:   len(headers),
			LineStart: lineStart,
			Data:      data,
		})
	}

	return tables
}

// parseTableRow parses a single table row
func parseTableRow(line string) []string {
	// Remove leading and trailing |
	line = strings.Trim(line, "|")
	// Split by | and trim each cell
	cells := strings.Split(line, "|")
	var result []string
	for _, cell := range cells {
		trimmed := strings.TrimSpace(cell)
		result = append(result, trimmed)
	}
	return result
}

// isTableSeparator checks if a line is a table separator
func isTableSeparator(line string) bool {
	// Remove leading and trailing |
	line = strings.Trim(line, "|")
	// Split by | and check each cell
	cells := strings.Split(line, "|")
	for _, cell := range cells {
		trimmed := strings.TrimSpace(cell)
		// Check if cell contains only -, spaces, and colons
		if !regexp.MustCompile(`^[\s\-:]+$`).MatchString(trimmed) {
			return false
		}
	}
	return true
}

// parseLists extracts list information from markdown lines
func parseLists(lines []string) []models.List {
	var lists []models.List
	listIndex := 0
	inList := false
	var listType string
	var items []string
	lineStart := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if line is a list item
		if isListItem(trimmed) {
			if !inList {
				// Start of new list
				inList = true
				lineStart = i + 1
				listType = getListType(trimmed)
				items = []string{trimmed}
			} else {
				// Continue existing list
				items = append(items, trimmed)
			}
		} else if inList {
			// End of list
			if len(items) > 0 {
				lists = append(lists, models.List{
					Type:      listType,
					Items:     len(items),
					LineStart: lineStart,
					Content:   items,
				})
				listIndex++
			}
			inList = false
			items = []string{}
		}
	}

	// Handle list at end of document
	if inList && len(items) > 0 {
		lists = append(lists, models.List{
			Type:      listType,
			Items:     len(items),
			LineStart: lineStart,
			Content:   items,
		})
	}

	return lists
}

// isListItem checks if a line is a list item
func isListItem(line string) bool {
	// Check for unordered list (-, *, +)
	if regexp.MustCompile(`^[\-\*\+]\s+`).MatchString(line) {
		return true
	}
	// Check for ordered list (1., 2., etc.)
	if regexp.MustCompile(`^\d+\.\s+`).MatchString(line) {
		return true
	}
	return false
}

// getListType determines if a list is ordered or unordered
func getListType(line string) string {
	if regexp.MustCompile(`^\d+\.\s+`).MatchString(line) {
		return "ordered"
	}
	return "unordered"
}

// countCodeBlocks counts the number of code blocks
func countCodeBlocks(lines []string) int {
	count := 0
	inCodeBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCodeBlock {
				// End of code block
				inCodeBlock = false
				count++
			} else {
				// Start of code block
				inCodeBlock = true
			}
		}
	}

	return count
}

// countLinks counts the number of markdown links
func countLinks(content string) int {
	// Count [text](url) links
	linkRegex := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	matches := linkRegex.FindAllString(content, -1)
	return len(matches)
}

// countImages counts the number of markdown images
func countImages(content string) int {
	// Count ![alt](url) images
	imageRegex := regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	matches := imageRegex.FindAllString(content, -1)
	return len(matches)
}

// countParagraphs counts the number of paragraphs
func countParagraphs(lines []string) int {
	count := 0
	inParagraph := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !isHeading(trimmed) && !isListItem(trimmed) && !strings.HasPrefix(trimmed, "```") && !strings.Contains(trimmed, "|") {
			if !inParagraph {
				count++
				inParagraph = true
			}
		} else {
			inParagraph = false
		}
	}

	return count
}

// isHeading checks if a line is a heading
func isHeading(line string) bool {
	headingRegex := regexp.MustCompile(`^(#{1,6})\s+`)
	return headingRegex.MatchString(line)
}
