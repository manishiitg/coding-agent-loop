package workflowtypes

import (
	"fmt"
	"regexp"
	"strings"
)

// KnowledgebaseSource attaches only another workflow's local KB, never its attachments.
type KnowledgebaseSource struct {
	WorkflowID string `json:"workflow_id"`
	Alias      string `json:"alias"`
	Access     string `json:"access"`
}

var kbAlias = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

func ValidateKnowledgebaseSources(sources []KnowledgebaseSource, ownID string) error {
	if len(sources) > 20 {
		return fmt.Errorf("at most 20 knowledgebase sources may be attached")
	}
	aliases, ids := map[string]bool{}, map[string]bool{}
	for _, source := range sources {
		if !kbAlias.MatchString(source.Alias) || source.Alias == "access" {
			return fmt.Errorf("KB alias must start with a lowercase letter and contain only lowercase letters, digits and underscores; access is reserved")
		}
		if source.WorkflowID == "" || strings.TrimSpace(source.WorkflowID) != source.WorkflowID || source.WorkflowID == ownID {
			return fmt.Errorf("KB source requires another workflow's ID")
		}
		if source.Access != "read" {
			return fmt.Errorf("shared knowledgebase access must be read")
		}
		if aliases[source.Alias] || ids[source.WorkflowID] {
			return fmt.Errorf("duplicate KB source or alias")
		}
		aliases[source.Alias] = true
		ids[source.WorkflowID] = true
	}
	return nil
}
