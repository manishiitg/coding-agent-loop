package step_based_workflow

import "testing"

func TestValidateFilePath(t *testing.T) {
	stepExecutionPath := "Workflow/linkedin/runs/iteration-0/default/execution/step-global-research"

	testCases := []struct {
		name      string
		fileName  string
		want      string
		wantError bool
	}{
		{
			name:     "step local bare file",
			fileName: "auth_context.json",
			want:     "Workflow/linkedin/runs/iteration-0/default/execution/step-global-research/auth_context.json",
		},
		{
			name:     "workflow knowledgebase path",
			fileName: "knowledgebase/research/global_trends.json",
			want:     "Workflow/linkedin/knowledgebase/research/global_trends.json",
		},
		{
			name:     "already workflow scoped relative path",
			fileName: "Workflow/linkedin/knowledgebase/research/global_trends.json",
			want:     "Workflow/linkedin/knowledgebase/research/global_trends.json",
		},
		{
			name:     "absolute path inside current workflow",
			fileName: "/app/workspace-docs/Workflow/linkedin/knowledgebase/research/global_trends.json",
			want:     "Workflow/linkedin/knowledgebase/research/global_trends.json",
		},
		{
			name:      "absolute path outside current workflow",
			fileName:  "/app/workspace-docs/Workflow/social-media/knowledgebase/research/global_trends.json",
			wantError: true,
		},
		{
			name:      "relative path outside current workflow",
			fileName:  "Workflow/social-media/knowledgebase/research/global_trends.json",
			wantError: true,
		},
		{
			name:      "path traversal rejected",
			fileName:  "../knowledgebase/research/global_trends.json",
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateFilePath(stepExecutionPath, tc.fileName)
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected error, got path %q", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
