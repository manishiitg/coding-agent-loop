package utils

// GetCommonFileInstructions returns common file operation instructions
// that are shared across all agent types and server configurations
func GetCommonFileInstructions() string {
	return `
If we a file is downloaded using playwright a file or take a screenshot, it will be stored in workspace -> Downloads folder.
`
}
