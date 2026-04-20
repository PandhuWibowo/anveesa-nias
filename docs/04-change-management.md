# Change Management

## Approval Requests

Route:
- `/approvals`

Purpose:
- Global queue for SQL write requests that require approval before execution.

Primary actions:
- Create approval request.
- Select workflow.
- Approve, reject, or execute approved request.

Suggested screenshots:
- `docs/screenshots/approval-requests-list.png`
- `docs/screenshots/approval-request-detail.png`

## Change Sets

Route:
- `/change-sets`

Purpose:
- Packages structured database changes with review and execution flow.

Primary actions:
- Create draft change set.
- Validate target.
- Submit draft.
- Approve and execute.

Suggested screenshots:
- `docs/screenshots/change-sets-page.png`
- `docs/screenshots/change-set-detail.png`

## Data Scripts

Route:
- `/data-scripts`

Purpose:
- Builds programmable data changes using JavaScript, Python, or PHP helper APIs.

Core concepts:
- A script is a reusable definition.
- A draft or request is created when a plan is generated from a saved version.
- `Preview Plan` is optional inspection.
- `Submit Draft` creates the plan and sends it into approval.

Primary actions:
- Create script.
- Save new version.
- Select connection, database, and workflow.
- Preview plan.
- Submit draft.
- Review and execute plan history for one script.

Suggested screenshots:
- `docs/screenshots/data-scripts-library.png`
- `docs/screenshots/data-scripts-editor.png`
- `docs/screenshots/data-scripts-schema-panel.png`
- `docs/screenshots/data-scripts-request-detail.png`

## Data Script Requests

Route:
- `/data-script-requests`

Purpose:
- Global queue for all data-script generated drafts and requests across all scripts.

Primary actions:
- Filter by status.
- Inspect request detail.
- Submit draft.
- Approve, reject, or execute.
- Jump back to the originating script.

Suggested screenshots:
- `docs/screenshots/data-script-requests-list.png`
- `docs/screenshots/data-script-request-detail.png`

## Schema Diff

Route:
- `/diff`

Purpose:
- Compares schema structure between environments or targets.

Screenshot:
- `docs/screenshots/schema-diff-page.png`
