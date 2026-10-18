# Nias design direction

Direction for the Nias app. It inherits the Anveesa family direction from anveesa.com (`anveesa-official-website/DESIGN.md`): personality, palette, type and shape come from there, and the app shell follows Vestra. This file records how that direction applies inside a work tool with many pages, where the content is the user's data and the interface has to step back.

Agents treat this file as design data, then apply `antislop` as the filter. Tokens live in `web/src/styles/main.css`. If code and this file disagree, fix one of them.

Dial: ENERGY 1 / RHYTHM 1 / MOTION 1

- ENERGY 1: tables, query results and logs are the focal point. Chrome is quiet so data reads clearly.
- RHYTHM 1: every page uses the same header, panel, empty state and spacing. Uniformity is deliberate: Nias has more than fifty pages, and users should learn one pattern once.
- MOTION 1: hover and press states, plus a short fade when a menu, palette or dialog opens. Nothing loops.

## Identity

Nias is the Anveesa studio for databases and the infrastructure around them: SQL, schemas, dashboards, approvals and audit, plus Redis, Kafka, search clusters, Docker, Kubernetes and servers over SSH. It is self-hosted and runs in the browser.

## Personality

Warm and approachable, like the rest of Anveesa. Plain words over jargon ("Servers", not "SSH hosts"; "Ask your data", not "AI analytics"), sentence case, and copy that says what will happen to the user's data. It should feel calm enough to run a production query in.

## Palette

Sky and navy from the Anveesa family. Nias's own signature is pink.

| Role | Token | Light | Dark | Used for |
|---|---|---|---|---|
| Canvas | `--canvas` | `#eef4fa` | `#070d18` | Window background, sidebar |
| Panel | `--bg` | `#ffffff` | `#0b1422` | The floating content panel |
| Surface | `--surface` | `#ffffff` | `#0f1a2c` | Cards, menus, dialogs, the palette |
| Surface 2 | `--surface-2` | `#f4f8fc` | `#14213a` | Inputs, table headers, subtle fills |
| Hover | `--hover` | navy 5.5% | sky-white 6% | Hover fill on rows and items |
| Border | `--border` | `#e3ebf3` | sky 9% | Dividers, 1px rings on cards |
| Border 2 | `--border-2` | `#d0dce8` | sky 17% | Input outlines, ghost buttons |
| Text | `--text` | `#0d1b2a` | `#e6eef8` | Primary text |
| Secondary text | `--text-2` | `#4b5d71` | `#a3b4c9` | Labels, icons at rest |
| Muted text | `--muted` | `#5f7186` | `#8a9cb4` | Hints, meta, timestamps, shortcut keys |
| Accent (sky) | `--accent` | `#0369a1` | `#38bdf8` | Primary button, active nav, focus ring, links |
| Nias signature | `--signature` | `#be185d` | `#f472b6` | The Nias mark only |
| Success | `--success` | `#166534` | `#4ade80` | Healthy, connected, done |
| Warning | `--warning` | `#b45309` | `#fbbf24` | Waiting, needs attention |
| Danger | `--danger` | `#be123c` | `#fb7185` | Errors, delete, drop |

Rules:

- **One accent.** Sky marks the one thing that is active or primary: the current page in the sidebar, the main action on a page, the focused control. Nothing else is sky.
- **Signature stays on the mark.** Pink appears on the Nias mark in the sidebar, the login screen and the favicon. Never on buttons or states, so it never competes with sky.
- **Status colors are for status.** Green, amber and red only describe a state (a healthy connection, a waiting approval, a failed job). They are never decoration.
- **Driver colors are content.** PostgreSQL blue, Redis red and the other driver colors appear only inside driver badges, the way provider colors do in Vestra.
- **Contrast.** Text tokens pass WCAG AA (4.5:1) on canvas, panel and surface in both themes.
- The theme follows the operating system until the user picks one. Both themes are first-class.

Old token names (`--bg-body`, `--bg-surface`, `--text-primary`, `--brand`, …) still exist as aliases so existing pages keep working. New code uses the family names above.

## Typography

- **UI: Figtree**, 14px base, 500 for labels, 600 for headings. Shared with anveesa.com and Workflow; its open shapes stay legible in dense tables and menus.
- **Display: Fraunces**, only where Nias speaks directly to the user: the sign-in heading and the greeting on Home. Never in page titles, toolbars, tables or dialogs.
- **Mono: IBM Plex Mono**, only for real code and values: SQL, query results, keys, hostnames and ports, JSON, shortcut keys. Never for titles or labels.

| Role | Size / weight |
|---|---|
| Page title | 22px / 600, tracking −0.015em |
| Card or panel title | 14.5px / 600 |
| Body, menu items, nav | 13.5–14px / 400–500 |
| Field label | 13px / 500, `--text-2` |
| Section label | 12–12.5px / 500, `--muted` |
| Meta | 12–12.5px / 400, `--muted` |

- Sentence case everywhere, including buttons ("New change set", not "New Change Set"). No uppercase tracked labels.
- Numbers in tables and stats use `font-variant-numeric: tabular-nums`.

## Shape and depth

- Radius scale: 6 (chips, badges, keys), 8 (buttons, inputs, menu items, cards inside a panel), 12 (the content panel, cards, menus, the palette), 16 (dialogs).
- Spacing scale: 4, 6, 8, 12, 16, 20, 24, 32px.
- Depth has four levels and no more:
  - Flat with a 1px `--border` ring: cards and panels at rest.
  - `--shadow-panel`: the floating content panel and the active sidebar item.
  - `--shadow-md`: menus and popovers.
  - `--shadow-lg`: dialogs and the command palette.
- No gradients, glows, glass or hover lifts. A card never has a border and a shadow at rest.
- Dot grids stay only on canvases where they align things (ER diagram, pipeline canvas, Docker topology).

## Layout

```
┌───────────────┬───────────────────────────────────────────────┐
│ ▮ Nias      ▯ │ SQL Studio  Objects  ER diagram  Schema diff … │  ← section tabs
│ ⌕ Search   ⌘K │ ───────────────────────────────────────────── │
│ ● shop-local ⇕│  Title                              actions   │
│               │  One line of context                          │
│ ⌂ Home        │                                               │
│ ─────────     │  Content                                      │
│ ▤ Database  ◀ │                                               │
│   Analytics   │                                               │
│   Changes     │                                               │
│   Automation  │                                               │
│   Monitoring  │                                               │
│   Infrastr.   │                                               │
│               │                                               │
│ 🔔 Notifs   3 │                                               │
│ ⚙ Settings    │                                               │
│ (A) admin   ☾ │                                               │
└───────────────┴───────────────────────────────────────────────┘
   canvas            floating content panel (8px gap, 12px radius)
```

### Navigation: places, not pages

Nias has more than fifty pages, but nobody needs fifty links. The sidebar lists **places**; a place's pages are **tabs** across the top of the content panel. One click picks the place, one click picks the page, and the palette (⌘K) jumps straight to any page by name.

| Place | Pages (tabs) |
|---|---|
| Home | Greeting, search, recent pages, connections |
| *Connection place* — named after the active connection ("Database", "Redis", "Search", "Kafka", "Storage", …) | Only the tools that work with that connection. Postgres: SQL Studio, Objects, ER diagram, Schema diff, Row history, Sessions, Logs, Users, Replication, Parameters. Redis: Redis, Laravel queue. Search: Indices, Policies. Buckets: Files. |
| Analytics | Overview, Dashboards, Saved queries, Ask your data |
| Changes | Approvals, Change sets, Data scripts, Script requests, Approval rules |
| Automation | Schedules, Pipelines, Backups, Watchers |
| Monitoring | Health, Query performance, Logs, Uptime, Infra metrics, Audit log |
| Infrastructure | Servers, Docker, Nginx, Cron, Files, Kubernetes, Clusters |
| Settings (sidebar footer) | Connections, Users, Roles, Access groups, Alert channels, Alert history, AI provider, Security |

Rules:

- **The connection place follows the connection switcher.** Switching to a Redis connection turns "Database" into "Redis" with Redis tools; no menu ever lists tools for a database you aren't using.
- **A place reopens where you left it.** Clicking Monitoring after reading the audit log returns to the audit log, not to Health.
- **Tabs only appear when a place has more than one page** the user may open. Pages the user lacks permission for never appear.
- **No page belongs to two places.** If a page seems to fit two, it goes where people look for it first (Logs live in Monitoring, not under the search cluster).
- A page header does not repeat the place name; the tabs already say where you are.
- Navigation lives in `web/src/navigation.ts` (`NAV_SECTIONS`). The sidebar, tabs, palette and Home all read it. A new page is added there, in exactly one section, with its permissions and (for connection tools) its drivers.

### Shell

- **Sidebar** sits on the canvas, 244px wide; it collapses to a 56px icon rail with ⌘\ / Ctrl+\. Top to bottom: the Nias mark, search (opens the palette), the connection switcher, Home, the places, then notifications, Settings, the account menu and the theme toggle.
- **Content panel** floats on the canvas with an 8px gap, 12px radius and `--shadow-panel`. Section tabs sit at its top edge; every page renders below them. On screens under 900px the panel goes edge to edge and the sidebar becomes a drawer behind a top bar that names the current place.
- **Page header** (`.page-hero`): title and one line of context on the left, actions on the right with the primary action last. Stats, when a page has them, sit on their own row under the title (`.page-metrics`).
- **Workspace pages** (SQL Studio, ER diagram, Objects, Dashboards, Kafka, Redis, Docker, Kubernetes) use a compact toolbar instead of a page header, so the data gets the height.

## Components

### Command palette
⌘K / Ctrl+K from anywhere. It finds pages, connections and actions, shows recent pages first, and only lists what the user may open. Enter opens, Esc closes, arrows move.

### Buttons
| Variant | Look | Use |
|---|---|---|
| Primary | Solid `--accent`, `--accent-fg` text | One per view: the main action |
| Ghost | Transparent, `--border-2` ring | Secondary actions |
| Danger | Red text, no fill | Delete, drop, flush. A red fill only inside a confirm dialog |
| Icon | 30px square, no ring, `--muted` icon | Toolbars and rows; always has a tooltip or `aria-label` |

Labels are verbs in sentence case: "Run", "Save script", "Add server". Never "OK", "Submit" or a leading "+".

### Empty and not-configured states (`EmptyState.vue`)
One muted icon, one line saying what is missing, one sentence on why it matters, and the one action that fixes it. A page that needs a particular kind of connection uses `ConnectionGate.vue`: it lists the matching connections for a one-click pick, or offers "Add Redis connection" with the driver already chosen. First run, filtered-to-nothing and not-permitted are different states with different copy.

### Lists and tables
- Header row on `--surface-2`, 12px / 500 labels in sentence case; sortable columns show their direction.
- Hover fill `--hover`; selected fill `--accent-bg`.
- At most two visible row actions (one quick action plus ⋯); long chip lists collapse to six with "+N more".

### Badges
6px radius, 12px / 500, tinted fill. A badge marks a state or a category, never decoration.

### Dialogs
16px radius, `--shadow-lg`, dimmed backdrop. Title in sentence case and a line saying what will happen. Footer right-aligned: Cancel, then the primary action. Enter submits, Esc cancels. On phones a dialog becomes a bottom sheet.

### Inputs
34px high (40px on touch), `--border-2` ring; focus shows the `--accent` ring plus a 3px `--accent-ring`. Every field has a visible label. Placeholders show an example, never an instruction.

### Toasts
Bottom right, one line where possible, an icon in the status color. Errors stay until dismissed.

## Motion

- 140ms ease on hover, press and color changes.
- Menus, the palette and dialogs fade and rise 4–6px on open.
- The sidebar collapses over 180ms; nothing else animates layout.
- Spinners and skeletons are the only things that move on their own, and only while something is loading.
- Under `prefers-reduced-motion`, transitions are removed.

## Writing

- Address the user as "you"; describe outcomes ("Runs every 5 minutes and alerts you when it fails").
- Name things the way people say them: servers, files, object storage, logs. Keep the technical word in the description when it helps ("Servers — SSH hosts used by Docker, Files, Nginx and Cron").
- Errors say what happened and what to do: "This database has no failed_jobs table. Pick the database your Laravel app uses."
- No exclamation marks, no emoji, no "Oops", no "seamless", "powerful" or "blazing fast".

## Accessibility

- Every control is a real `<button>` or link with visible text or an `aria-label`.
- Focus ring: 2px sky outline with 2px offset via `:focus-visible`. Never removed without a replacement.
- Escape closes menus, the palette, drawers and dialogs.
- Touch targets are at least 38–40px under 720px wide; no page scrolls sideways at 390px.

## Anti-patterns (reject in review)

- A second accent color, or the pink signature on anything but the mark.
- Gradient buttons, gradient text, gradient or radial-glow backgrounds.
- ALL-CAPS or letter-spaced labels; monospace titles.
- Emoji as icons; icons without a label or tooltip.
- Hero cards with stats floating beside the title; stat tiles with invented numbers.
- Cards inside cards; border plus shadow on the same element at rest; hover lifts.
- A page that shows an empty select or a "No connection" sentence with no way forward.
- Decorative left stripes. A colored edge is allowed only when it marks a real state (log severity, diff added or removed).
