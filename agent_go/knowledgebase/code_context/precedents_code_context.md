# Code Context: precedents
Generated from: Confida/confida-v2 staging branch

Files analyzed:
- app/services/review/api/precedent/precedent-typed.ts
- app/services/review/api/precedent/precedent-groups-typed.ts
- app/services/review/api/precedent/schemas.ts
- app/services/review/api/precedent/precedent-group-schemas.ts
- app/ui/admin/src/components/precedents/AddPrecedent.tsx
- app/ui/admin/src/components/precedents/AddPrecedentModal.tsx
- app/ui/admin/src/components/precedents/AllGroupsPage.tsx
- app/ui/admin/src/components/precedents/NewGroupDialog.tsx
- app/ui/admin/src/components/precedents/MovePrecedentDialog.tsx
- app/ui/admin/src/components/precedents/DeleteGroupDialog.tsx

## API Endpoints
- GET /api/review/precedents/search
- GET /api/review/precedents/internal/search
- GET /api/review/precedents/internal/groups
- GET /api/review/precedents/internal/list
- GET /api/review/precedents/internal/content
- POST /api/review/precedents/internal/add-to-group
- POST /api/review/precedents/internal/remove-from-group
- POST /api/review/precedents/extract-metadata
- GET /api/review/precedents
- POST /api/review/precedents
- GET /api/review/precedents/:id
- PATCH /api/review/precedents/:id
- POST /api/review/precedents/:id/documents/:documentId
- DELETE /api/review/precedents/:id/documents/:documentId
- DELETE /api/review/precedents/:id
- GET /api/review/precedent-groups
- GET /api/review/precedent-groups/:id
- POST /api/review/precedent-groups
- PATCH /api/review/precedent-groups/:id
- DELETE /api/review/precedent-groups/:id
- POST /api/review/precedent-groups/:id/precedents/:precedentId
- DELETE /api/review/precedent-groups/:id/precedents/:precedentId

## Validation Rules
- `PrecedentPartySchema.id`: `z.uuid()`
- `PrecedentDocumentSchema.id`: `z.uuid()`
- `PrecedentDocumentSchema.documentId`: `z.uuid()`
- `PrecedentSchema.id`: `z.uuid()`
- `CreatePrecedentRequestSchema.name`: `z.string()`
- `CreatePrecedentRequestSchema.comments`: `z.string().optional()`
- `CreatePrecedentRequestSchema.contractType`: `z.string().optional()`
- `CreatePrecedentRequestSchema.parties[]`: `{ name: z.string() }` (optional array)
- `UpdatePrecedentRequestSchema`: partial of create request
- `CreatePrecedentGroupRequestSchema.groupName`: `z.string().min(1)`
- `CreatePrecedentGroupRequestSchema.description`: `z.string().max(200).optional()`
- `UpdatePrecedentGroupRequestSchema.groupName`: `z.string().min(1).optional()`
- `UpdatePrecedentGroupRequestSchema.description`: `z.string().max(200).optional()`
- `PrecedentGroupSchema.description`: `z.string().max(200).nullable().optional()`
- `PrecedentGroupSchema.createdAt`: `z.string().datetime()`
- `PrecedentGroupSchema.updatedAt`: `z.string().datetime()`
- `/internal/search.query.limit`: `z.coerce.number().optional().default(20)`
- `/internal/list.query.ungroupedOnly`: `z.coerce.boolean().optional().default(false)`
- `/internal/list.query.groupId`: `z.string().trim().min(1).optional()`
- `/extract-metadata.body.model`: `z.string().optional().default('gemini-1.5-flash')`
- UI constraint in `NewGroupDialog.tsx`: `MAX_DESCRIPTION_LENGTH = 200`

## Exact Error Messages
- "Failed to search precedents"
- "Failed to list precedent groups"
- "Failed to list precedents"
- "Either precedentId or precedentName is required."
- "Precedent ${label} not found."
- "Failed to get precedent content"
- "Failed to add precedent to group"
- "Failed to remove precedent from group"
- "Failed to extract metadata"
- "Failed to fetch precedents"
- "Failed to create precedent"
- "Precedent not found"
- "Failed to fetch precedent details"
- "Failed to update precedent"
- "Failed to link document"
- "Failed to unlink document"
- "Failed to delete precedent"
- "Precedent group not found"
- "Failed to fetch precedent group details"
- "Failed to create precedent group"
- "Failed to update precedent group"
- "Failed to delete precedent group"
- "Precedent is not in this group"
- "Name is required"
- "Group name is required"
- "Failed to create group"
- "Failed to update group"
- "Failed to delete group"
- "Failed to move precedent"

## RBAC / Permissions
- `/api/review/precedents` POST requires `requiredRoles: 'admin'`
- `/api/review/precedents/:id` PATCH requires `requiredRoles: 'admin'`
- `/api/review/precedents/:id/documents/:documentId` POST requires `requiredRoles: 'admin'`
- `/api/review/precedents/:id/documents/:documentId` DELETE requires `requiredRoles: 'admin'`
- `/api/review/precedents/:id` DELETE requires `requiredRoles: 'admin'`
- `/api/review/precedent-groups` POST requires `requiredRoles: 'admin'`
- `/api/review/precedent-groups/:id` PATCH requires `requiredRoles: 'admin'`
- `/api/review/precedent-groups/:id` DELETE requires `requiredRoles: 'admin'`
- `/api/review/precedent-groups/:id/precedents/:precedentId` POST requires `requiredRoles: 'admin'`
- `/api/review/precedent-groups/:id/precedents/:precedentId` DELETE requires `requiredRoles: 'admin'`
- Service-only internal routes use `auth: 'service'`:
- `/api/review/precedents/internal/search`
- `/api/review/precedents/internal/groups`
- `/api/review/precedents/internal/list`
- `/api/review/precedents/internal/content`
- `/api/review/precedents/internal/add-to-group`
- `/api/review/precedents/internal/remove-from-group`

## UI States
- `AllGroupsPage.tsx`: loading state via `FullPageLoader` when `isLoading && groups.length === 0`
- `AllGroupsPage.tsx`: empty state text "No groups found."
- `MovePrecedentDialog.tsx`: loading text "Loading groups..."
- `MovePrecedentDialog.tsx`: empty states "No groups match your search" and "No other groups available"
- `PrecedentTable.tsx`: loading state via `FullPageLoader` when `isLoading && precedents.length === 0 && groups.length === 0`
- `PrecedentTable.tsx`: empty state text "No precedents found" and "No precedents found matching your search."
- `AddPrecedent.tsx` / `AddPrecedentModal.tsx`: submit/loading flags with `isSubmitting` and `isProcessing`; primary action disabled while processing or without files
- `NewGroupDialog.tsx`: create/save button disabled when `!groupName.trim()` or while `isSubmitting`
- `DeleteGroupDialog.tsx`: delete action displays `Deleting...` while deleting

## Business Logic Branches
- Internal search company resolution:
- If `companyId` not provided and `userId` is present, code fetches user and derives `targetCompanyId`.
- Internal list filtering:
- If `groupId` is provided, filter by `precedentGroupId = groupId`; else if `ungroupedOnly`, filter `precedentGroupId = null`; else return all company precedents.
- Internal content lookup:
- Requires either `precedentId` or `precedentName`; otherwise returns 400 with exact error.
- Internal content truncation:
- Document aggregation stops at `MAX_CHARS = 30000`.
- Precedent update with parties:
- If `parties` is provided, existing parties are deleted and replaced.
- Group rename propagation:
- On group PATCH with `groupName`, all linked precedents are updated (`precedentGroupName`) in a transaction.
- Group delete behavior:
- Deleting a group unlinks precedents (`precedentGroupId` and `precedentGroupName` set to null) and then deletes the group.
- Remove-from-group guard:
- If the precedent is not currently in the specified group, returns 404 "Precedent is not in this group".
- Vector sync side effects are fire-and-forget after primary DB operations for create/update/link/unlink/group changes.

## Feature Flags
- No explicit feature-flag gating found in the analyzed files.
