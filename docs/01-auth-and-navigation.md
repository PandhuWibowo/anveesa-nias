# Authentication And Navigation

## Login

Route:
- `/login`

Purpose:
- Authenticates a user when application auth is enabled.

Primary actions:
- Enter username and password.
- Sign in.

Expected result:
- User is redirected into the app.

Screenshot:
- `docs/screenshots/login-page.png`

## Welcome

Route:
- `/welcome`

Purpose:
- Landing page after login or permission fallback page.

Primary actions:
- Review quick product orientation.
- Navigate into available sections.

Screenshot:
- `docs/screenshots/welcome-page.png`

## Top Navigation

Purpose:
- Provides access to product sections: Overview, Explore, Monitor, Change, Admin.

Primary elements:
- Brand and version.
- Connection picker.
- Global navigation menus.
- User/account menu.

Screenshot:
- `docs/screenshots/top-navigation.png`

## Connection Picker

Purpose:
- Switches the active database connection context used by many screens.

Primary actions:
- Open connection dropdown.
- Select a connection.

Expected result:
- Active connection label changes.
- Connection-sensitive screens refresh around the new target.

Screenshot:
- `docs/screenshots/connection-picker.png`
