I have implemented the progress bar for workspace backup imports in the frontend.

Key changes:
1.  Modified `frontend/src/services/api.ts` to support an `onProgress` callback in `importWorkflowBackup`.
2.  Updated `frontend/src/components/sidebar/WorkflowBackupSection.tsx` to track and display upload progress with a progress bar.
3.  Updated `frontend/src/components/Workspace.tsx` and `frontend/src/components/workspace/PlannerFileList.tsx` to show import progress in the main workspace view as well.

You can now test the import functionality and see the progress bar for large files.