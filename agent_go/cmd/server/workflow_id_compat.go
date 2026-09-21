package server

import "encoding/json"

// Old persisted requests can outlive a deployment. New responses and writes use
// workflow_id; only decoding accepts the former wire key.
const oldWorkflowIDKey = "preset_query_id"

func readOldWorkflowID(data []byte, current string) string {
	if current != "" {
		return current
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return current
	}
	var workflowID string
	if json.Unmarshal(fields[oldWorkflowIDKey], &workflowID) != nil {
		return current
	}
	return workflowID
}

func (req *QueryRequest) UnmarshalJSON(data []byte) error {
	type plain QueryRequest
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*req = QueryRequest(decoded)
	req.WorkflowID = readOldWorkflowID(data, req.WorkflowID)
	return nil
}

func (req *WorkflowRequest) UnmarshalJSON(data []byte) error {
	type plain WorkflowRequest
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*req = WorkflowRequest(decoded)
	req.WorkflowID = readOldWorkflowID(data, req.WorkflowID)
	return nil
}

func (req *WorkflowUpdateRequest) UnmarshalJSON(data []byte) error {
	type plain WorkflowUpdateRequest
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*req = WorkflowUpdateRequest(decoded)
	req.WorkflowID = readOldWorkflowID(data, req.WorkflowID)
	return nil
}
