package step_based_workflow

import (
	"fmt"
	"regexp"
	"strings"
)

// GenerateValidationSchemaFromSuccessCriteria attempts to generate a validation schema
// from success criteria text. This is a best-effort parser that extracts file names
// and field requirements from natural language success criteria.
//
// Returns nil if the criteria cannot be parsed into a structured schema.
// This is intentional - not all success criteria can be automatically converted.
func GenerateValidationSchemaFromSuccessCriteria(successCriteria string) *ValidationSchema {
	if successCriteria == "" {
		return nil
	}

	schema := &ValidationSchema{
		Files: []FileValidationRule{},
	}

	// Pattern 1: "File {filename} contains {field} field"
	// Pattern 2: "File {filename} contains {field} field set to '{value}'"
	// Pattern 3: "File {filename} includes: {list of fields}"
	// Pattern 4: Multiple files mentioned with AND/OR

	// Extract file names (common patterns)
	filePattern := regexp.MustCompile(`(?i)file\s+['"]?([a-zA-Z0-9_\-\.]+\.json)['"]?`)
	fileMatches := filePattern.FindAllStringSubmatch(successCriteria, -1)

	if len(fileMatches) == 0 {
		// Try without "file" prefix - look for .json files
		jsonFilePattern := regexp.MustCompile(`['"]?([a-zA-Z0-9_\-\.]+\.json)['"]?`)
		fileMatches = jsonFilePattern.FindAllStringSubmatch(successCriteria, -1)
	}

	// Extract field names mentioned
	fieldPattern := regexp.MustCompile(`(?i)(?:field|key|property|contains?|includes?|has|with)\s+['"]?([a-zA-Z0-9_\-]+)['"]?`)
	fieldMatches := fieldPattern.FindAllStringSubmatch(successCriteria, -1)

	// Extract array mentions
	arrayPattern := regexp.MustCompile(`(?i)(?:array|list)\s+['"]?([a-zA-Z0-9_\-]+)['"]?`)
	arrayMatches := arrayPattern.FindAllStringSubmatch(successCriteria, -1)

	// Extract count requirements
	countPattern := regexp.MustCompile(`(?i)(?:at least|minimum|>=|≥)\s*(\d+)`)
	countMatches := countPattern.FindAllStringSubmatch(successCriteria, -1)

	// Extract "equals" or "matches" requirements
	equalsPattern := regexp.MustCompile(`(?i)(?:equals?|matches?|==)\s+(?:array\s+)?length|count\s+equals?`)
	hasEqualsCheck := equalsPattern.MatchString(successCriteria)

	// Process each file mentioned
	fileMap := make(map[string]*FileValidationRule)
	for _, match := range fileMatches {
		if len(match) < 2 {
			continue
		}
		file := match[1]
		if fileMap[file] == nil {
			fileMap[file] = &FileValidationRule{
				FileName:   file,
				MustExist:  true,
				JSONChecks: []JSONValidationCheck{},
			}
		}
	}

	// If no files found, return nil (can't generate schema)
	if len(fileMap) == 0 {
		return nil
	}

	// Add field checks for each file
	for _, fileRule := range fileMap {
		// Add checks for mentioned fields
		seenFields := make(map[string]bool)
		for _, match := range fieldMatches {
			if len(match) < 2 {
				continue
			}
			fieldName := match[1]
			// Skip common words
			if isCommonWord(fieldName) {
				continue
			}
			if seenFields[fieldName] {
				continue
			}
			seenFields[fieldName] = true

			check := JSONValidationCheck{
				Path:      fmt.Sprintf("$.%s", fieldName),
				MustExist: true,
			}

			// Check if it's mentioned as an array
			for _, arrayMatch := range arrayMatches {
				if len(arrayMatch) >= 2 && arrayMatch[1] == fieldName {
					check.ValueType = "array"
					// Add min length if count requirement found
					if len(countMatches) > 0 {
						// Try to extract number (simplified - just take first match)
						// In a real implementation, we'd need more sophisticated parsing
					}
					break
				}
			}

			// If no type determined, default to checking existence only
			if check.ValueType == "" {
				// Try to infer type from context
				if strings.Contains(strings.ToLower(successCriteria), fmt.Sprintf("%s array", fieldName)) ||
					strings.Contains(strings.ToLower(successCriteria), fmt.Sprintf("array %s", fieldName)) {
					check.ValueType = "array"
				} else if strings.Contains(strings.ToLower(successCriteria), fmt.Sprintf("%s object", fieldName)) ||
					strings.Contains(strings.ToLower(successCriteria), fmt.Sprintf("object %s", fieldName)) {
					check.ValueType = "object"
				} else {
					// Default to string for field existence checks
					check.ValueType = "string"
					check.MinLength = intPtr(1)
				}
			}

			fileRule.JSONChecks = append(fileRule.JSONChecks, check)
		}

		// Add array checks
		for _, arrayMatch := range arrayMatches {
			if len(arrayMatch) < 2 {
				continue
			}
			arrayName := arrayMatch[1]
			if isCommonWord(arrayName) {
				continue
			}

			// Check if we already added this field
			alreadyAdded := false
			for _, check := range fileRule.JSONChecks {
				if check.Path == fmt.Sprintf("$.%s", arrayName) {
					alreadyAdded = true
					break
				}
			}
			if alreadyAdded {
				continue
			}

			check := JSONValidationCheck{
				Path:      fmt.Sprintf("$.%s", arrayName),
				MustExist: true,
				ValueType: "array",
			}

			// Add min length if count requirement found
			if len(countMatches) > 0 {
				// Extract first number as min length (simplified)
				// In production, we'd need better parsing to match counts to specific arrays
			}

			fileRule.JSONChecks = append(fileRule.JSONChecks, check)
		}

		// Add consistency checks if "equals length" or "matches count" is mentioned
		if hasEqualsCheck {
			// Look for count fields and array fields that should match
			for _, fieldMatch := range fieldMatches {
				if len(fieldMatch) < 2 {
					continue
				}
				fieldName := fieldMatch[1]
				if strings.Contains(strings.ToLower(fieldName), "count") {
					// Find corresponding array (simplified - look for common patterns)
					for _, arrayMatch := range arrayMatches {
						if len(arrayMatch) < 2 {
							continue
						}
						arrayName := arrayMatch[1]
						// Add consistency check
						check := JSONValidationCheck{
							Path: fmt.Sprintf("$.%s", fieldName),
							ConsistencyCheck: &ConsistencyRule{
								Type:            "array_length",
								CompareWithPath: fmt.Sprintf("$.%s", arrayName),
							},
						}
						fileRule.JSONChecks = append(fileRule.JSONChecks, check)
						break
					}
				}
			}
		}

		// Only add file rule if it has checks
		if len(fileRule.JSONChecks) > 0 {
			schema.Files = append(schema.Files, *fileRule)
		}
	}

	// Return schema only if we found files and generated checks
	if len(schema.Files) > 0 {
		return schema
	}

	return nil
}

// isCommonWord checks if a word is a common English word that shouldn't be treated as a field name
func isCommonWord(word string) bool {
	commonWords := map[string]bool{
		"file": true, "files": true, "the": true, "a": true, "an": true,
		"contains": true, "include": true, "includes": true, "has": true,
		"with": true, "field": true, "fields": true, "key": true, "keys": true,
		"and": true, "or": true, "is": true, "are": true, "set": true, "to": true,
		"must": true, "should": true, "be": true, "present": true, "exists": true,
		"array": true, "arrays": true, "list": true, "lists": true, "object": true,
		"objects": true, "count": true, "length": true, "equals": true, "matches": true,
	}
	return commonWords[strings.ToLower(word)]
}

// intPtr returns a pointer to an int
func intPtr(i int) *int {
	return &i
}
