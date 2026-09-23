package step_based_workflow

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
)

// The Builder guidance's crew example must validate against the real
// add_crew_step schema; the old text omitted id/title and agents guessed.
func TestPlanEditingGuidanceCrewExampleValidates(t *testing.T) {
	raw, err := os.ReadFile("../../../../../cmd/server/guidance/templates/system/plan-editing-tools.md")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile("add_step\\(type=\"crew\", step=(\\{.*?\\})\\)`").FindSubmatch(raw)
	if match == nil {
		t.Fatal("crew add_step example not found in plan-editing-tools.md")
	}
	var step map[string]interface{}
	if err := json.Unmarshal(match[1], &step); err != nil {
		t.Fatalf("crew example is not valid JSON: %v", err)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal([]byte(getAddCrewStepSchema()), &schema); err != nil {
		t.Fatal(err)
	}
	schema["additionalProperties"] = false
	validator, err := compilePlanToolSchema(schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.Validate(step); err != nil {
		t.Fatalf("documented crew example does not validate: %v", err)
	}
}
