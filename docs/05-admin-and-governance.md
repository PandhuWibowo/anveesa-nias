# Admin And Governance

Admin and governance features control who can access the app, which database connections they can use, and what review rules apply to risky work.

## Connections

Route:
- `/connections`

Purpose:
- Manages database connection definitions and access grouping.
- Keeps database targets visible and reusable across the product.

Use cases:
- Add a new reporting database connection.
- Test whether credentials and network access work.
- Organize connections into folders.
- Change connection visibility or ownership rules.
- Prepare a connection for dashboards, saved queries, or approvals.

Typical workflow:
1. Open connections.
2. Create or edit a connection.
3. Choose driver and connection details.
4. Test the connection.
5. Save it and assign folder or visibility settings as needed.

Expected result:
- Users with access can select the connection from connection-aware screens.

Notes:
- Store only credentials that are needed.
- Use least privilege database users where possible.
- Connection access should match the user's real responsibility.

Screenshot:
- `docs/screenshots/connections-page.png`

## Users

Route:
- `/users`

Purpose:
- Manages application users and their high-level access.

Use cases:
- Create an account for a teammate.
- Deactivate a user who should no longer access the app.
- Assign a role.
- Review user status during access audits.

Typical workflow:
1. Open users.
2. Create or edit a user.
3. Assign role and account status.
4. Save changes.

Expected result:
- User access changes affect login and route availability.

Screenshot:
- `docs/screenshots/users-page.png`

## Permissions

Route:
- `/permissions`

Purpose:
- Manages roles, folders, and connection-level permission policy.
- Controls both app-level features and database operation permissions.

Use cases:
- Create a read-only analyst role.
- Restrict a group to specific connection folders.
- Allow query execution but block write operations.
- Review who has access to sensitive connections.
- Apply different access levels for different teams.

Typical workflow:
1. Define app roles.
2. Assign users to roles.
3. Create connection folders or groups.
4. Assign users and connections to groups.
5. Configure database permissions such as select, insert, update, delete, DDL, and admin operations.

Expected result:
- Users only see and use the features and connections they are allowed to access.

Notes:
- Prefer granting the minimum access required.
- Review permissions after team or responsibility changes.
- Permission changes should be tested with a non-admin account.

Screenshot:
- `docs/screenshots/permissions-page.png`

## Approval Workflows

Route:
- `/workflows`

Purpose:
- Configures approval routing by connection and access group.
- Defines who must approve high-risk SQL, data scripts, or change-set activity.

Use cases:
- Require database owner approval for write SQL.
- Route requests to different approvers by connection.
- Add multi-step approval for sensitive changes.
- Separate requester and approver responsibilities.

Typical workflow:
1. Create a workflow.
2. Assign relevant connections.
3. Assign groups or users.
4. Configure approval steps.
5. Save and test with a sample request.

Expected result:
- Requests use the configured approval chain before execution.

Notes:
- Keep workflows understandable.
- Avoid approval chains so complex that users bypass the process.
- Document who owns each workflow.

Suggested screenshots:
- `docs/screenshots/workflows-page.png`
- `docs/screenshots/workflow-editor.png`

## Security

Route:
- `/security`

Purpose:
- Provides security-related user or system controls.

Use cases:
- Manage account security settings.
- Review 2FA-related controls.
- Check security posture for the current user.

Typical workflow:
1. Open security.
2. Review available controls.
3. Enable or update security settings.
4. Confirm the account still works as expected.

Expected result:
- Users and administrators have a dedicated place for security-sensitive settings.

Screenshot:
- `docs/screenshots/security-page.png`
