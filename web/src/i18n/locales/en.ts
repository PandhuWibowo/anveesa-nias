// Source strings. Every key used with t() lives here; other locales may leave
// any key out and it falls back to this text.
//
// Navigation labels are not listed here: they come from navigation.ts and are
// translated with tr('nav.<key>', label) — see the nav.* keys in zh-CN.ts.

const en = {
  // Common
  'common.cancel': 'Cancel',
  'common.confirm': 'Confirm',
  'common.close': 'Close',
  'common.search': 'Search',
  'common.active': 'Active',
  'common.show': 'Show',
  'common.hide': 'Hide',

  // Language
  'language.label': 'Language',
  'language.choose': 'Choose language',

  // Shell
  'shell.home': 'Home',
  'shell.notifications': 'Notifications',
  'shell.settings': 'Settings',
  'shell.documentation': 'Documentation',
  'shell.passwordAnd2fa': 'Password and 2FA',
  'shell.signOut': 'Sign out',
  'shell.user': 'User',
  'shell.expandSidebar': 'Expand sidebar',
  'shell.collapseSidebar': 'Collapse sidebar',
  'shell.openMenu': 'Open menu',
  'shell.closeMenu': 'Close menu',
  'shell.searchShortcut': 'Search ({key})',
  'shell.noConnection': 'No connection',
  'shell.chooseToStart': 'Choose one to start',
  'shell.chooseConnection': 'Choose a connection',
  'shell.lightTheme': 'Switch to light theme',
  'shell.darkTheme': 'Switch to dark theme',
  'shell.primaryNav': 'Primary',

  // Command palette
  'palette.placeholder': 'Jump to a page, connection, or action',
  'palette.dialogLabel': 'Go to page or connection',
  'palette.recent': 'Recent',
  'palette.pages': 'Pages',
  'palette.connections': 'Connections',
  'palette.actions': 'Actions',
  'palette.noResults': 'Nothing matches “{q}”. Try a page name like “docker” or a connection name.',
  'palette.addConnection': 'Add connection',
  'palette.openDocs': 'Open documentation',
  'palette.security': 'Security and 2FA',
  'palette.hint.connections': 'Connections',
  'palette.hint.appearance': 'Appearance',
  'palette.hint.help': 'Help',
  'palette.hint.account': 'Account',
  'palette.hint.language': 'Language',
  'palette.switchLanguage': 'Switch language to {name}',
  'palette.navigate': 'to navigate',
  'palette.open': 'to open',

  // Sign in
  'auth.signIn': 'Sign in',
  'auth.signingIn': 'Signing in…',
  'auth.twoStep': 'Two-step verification',
  'auth.signInHelp': 'Use the account your administrator gave you.',
  'auth.twoStepHelp': 'Enter the 6-digit code from your authenticator app, or a backup code, for {user}.',
  'auth.username': 'Username',
  'auth.password': 'Password',
  'auth.showPassword': 'Show password',
  'auth.hidePassword': 'Hide password',
  'auth.code': 'Verification code',
  'auth.verify': 'Verify',
  'auth.verifying': 'Verifying…',
  'auth.differentAccount': 'Use a different account',
  'auth.err.missing': 'Enter your username and password.',
  'auth.err.missingCode': 'Enter the code from your authenticator app.',
  'auth.err.mismatch': 'That username and password don’t match.',

  // Home
  'home.morning': 'Good morning',
  'home.afternoon': 'Good afternoon',
  'home.evening': 'Good evening',
  'home.greeting': '{part}, {name}',
  'home.search': 'Jump to a page, connection, or action',
  'home.recent': 'Pick up where you left off',
  'home.connections': 'Connections',
  'home.manageAll': 'Manage all {n}',
  'home.noConnections': 'No connections yet',
  'home.noConnectionsAdmin': 'Add a database, cache, queue, or bucket to start browsing and querying it here.',
  'home.noConnectionsUser': 'Ask an administrator to share a connection with you.',
  'home.addConnection': 'Add connection',

  // Connection gate
  'gate.emptyTitle': 'No {label} connections yet',
  'gate.emptyText': 'Add {a} {label} connection {purpose}.',
  'gate.add': 'Add {label} connection',
  'gate.chooseTitle': 'Choose a {label} connection',
  'gate.chooseText': 'Pick one {purpose}.',
} as const

export type MessageKey = keyof typeof en
export default en as Record<MessageKey, string>
