# Change Management

Change management features help teams control risky database changes, review generated plans, and keep an approval trail.

## Approval Requests

Route:
- `/approvals`

Purpose:
- Provides a global queue for SQL write requests that require approval before execution.
- Separates request creation from approval and execution.

Use cases:
- A developer needs to run a data correction query.
- A support team member requests a one-off update.
- A database owner wants approval history before allowing a write operation.
- A team needs to prevent direct execution of high-risk SQL.

Typical workflow:
1. Create an approval request with SQL and target context.
2. Select the relevant workflow when required.
3. Submit the request.
4. Approvers review the SQL and context.
5. The request is approved, rejected, or executed once approved.

Expected result:
- Risky changes have a visible review trail.
- Execution is gated by the configured approval process.

Review checklist:
- Confirm the target connection.
- Confirm whether the SQL is scoped by primary key or precise filters.
- Confirm expected affected row count.
- Confirm rollback or correction path for risky changes.

Suggested screenshots:
- `docs/screenshots/approval-requests-list.png`
- `docs/screenshots/approval-request-detail.png`

## Change Sets

Route:
- `/change-sets`

Purpose:
- Packages structured database changes with review and execution flow.
- Makes larger changes easier to validate before they are applied.

Use cases:
- Bundle multiple related SQL statements into one reviewed unit.
- Prepare a repeatable migration-style data correction.
- Validate change intent before approval.
- Keep a record of what was planned and what was executed.

Typical workflow:
1. Create a draft change set.
2. Add SQL steps or structured changes.
3. Validate the target and expected behavior.
4. Submit for review.
5. Approve and execute when ready.

Expected result:
- A change set moves through a clear lifecycle from draft to execution.

Notes:
- Keep change sets focused.
- Prefer smaller, reversible changes over large mixed-purpose batches.

Suggested screenshots:
- `docs/screenshots/change-sets-page.png`
- `docs/screenshots/change-set-detail.png`

## Data Scripts

Route:
- `/data-scripts`

Purpose:
- Builds programmable data changes using JavaScript, Python, or PHP helper APIs.
- Supports more complex data plans than plain SQL when transformation logic is needed.

Use cases:
- Generate a plan from business rules.
- Transform source rows before writing target rows.
- Build repeatable repair scripts with reviewable output.
- Prepare a data migration where each planned change should be inspected.

Core concepts:
- A script is a reusable definition.
- A version captures the saved script content.
- A draft or request is created when a plan is generated from a saved version.
- `Preview Plan` lets users inspect generated operations before submission.
- `Submit Draft` sends the plan into approval.

Typical workflow:
1. Create or open a script.
2. Write script logic using supported helper APIs.
3. Save a new version.
4. Select connection, database, and workflow.
5. Preview the plan.
6. Submit the draft.
7. Review, approve, and execute the generated request.

Expected result:
- Complex data changes become explicit, inspectable plans.

Notes:
- Keep scripts deterministic where possible.
- Avoid relying on external state that reviewers cannot inspect.
- Document assumptions in script comments.

Suggested screenshots:
- `docs/screenshots/data-scripts-library.png`
- `docs/screenshots/data-scripts-editor.png`
- `docs/screenshots/data-scripts-schema-panel.png`
- `docs/screenshots/data-scripts-request-detail.png`

## Data Script Requests

Route:
- `/data-script-requests`

Purpose:
- Global queue for all data-script generated drafts and requests.
- Helps reviewers manage script-generated change plans across scripts.

Use cases:
- Review all pending generated plans.
- Continue a draft created from a data script.
- Approve, reject, or execute a generated plan.
- Jump back to the originating script for context.

Typical workflow:
1. Open data script requests.
2. Filter by status.
3. Inspect request detail and generated operations.
4. Submit draft, approve, reject, or execute based on role and state.

Expected result:
- Script-generated changes remain traceable and reviewable.

Suggested screenshots:
- `docs/screenshots/data-script-requests-list.png`
- `docs/screenshots/data-script-request-detail.png`

## Schema Diff

Route:
- `/diff`

Purpose:
- Compares schema structure between targets.
- Helps users understand structural differences before planning changes.

Use cases:
- Compare two environments.
- Find missing tables, columns, or indexes.
- Review drift before a migration.
- Produce a schema comparison summary for teammates.

Typical workflow:
1. Select source and target.
2. Run the comparison.
3. Review differences by object type.
4. Use the result to plan follow-up changes.

Expected result:
- Users get a clear list of schema differences.

Screenshot:
- `docs/screenshots/schema-diff-page.png`
