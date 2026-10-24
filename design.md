# Design — Haven

The locked design system for Haven's web UI and TV mode. Every screen follows
this file. To change the look, amend this file first, then the code.

## Idea: Faceplate

Haven looks like the front panel of a piece of server hardware, not a generic
dashboard. Panels are plates with hairline edges, labels are engraved, status
is shown with LEDs, numbers are instrument readouts, and buttons are keycaps.
One signal-orange accent marks what matters. Nothing is decorative unless it
belongs on a real faceplate.

## Genre

Modern-minimal, technical register. Custom theme (not from a catalog).

## Layout families

- App pages (Overview, Apps): Workbench. A faceplate header strip with a mode
  selector, then plates. Overview leads with a hairline-gridded meter bank,
  then a board of widget cards, then the apps.
- Updates: summary tiles first, then always in the same order: Haven, apps,
  server software. Sections never move to put the urgent one first.
- Server and Settings: a row of tabs in a well, one section group per tab.
- Catalog: Catalogue. A category index on the left, grouped rows on the right,
  a spec-sheet panel for installing.
- TV: a 10-foot launcher. Meter bank on top, then horizontal rows of large
  tiles navigated with a remote.

## Theme

Two faceplates, switched in Settings and stored per browser.

| Token | Graphite (default) | Bone (light) |
| --- | --- | --- |
| `--paper` | oklch(17% 0.006 60) | oklch(90.5% 0.012 82) |
| `--plate` | oklch(20.5% 0.007 60) | oklch(94.5% 0.009 82) |
| `--raised` | oklch(24% 0.008 60) | oklch(97.5% 0.006 82) |
| `--well` | oklch(13.5% 0.005 60) | oklch(87% 0.013 80) |
| `--ink` | oklch(93% 0.008 75) | oklch(21% 0.012 60) |
| `--ink-2` | oklch(76% 0.01 70) | oklch(36% 0.012 60) |
| `--muted` | oklch(64% 0.01 70) | oklch(46% 0.012 60) |
| `--rule` | oklch(30% 0.008 60) | oklch(79% 0.014 72) |
| `--accent` | oklch(70% 0.18 45) | oklch(66% 0.19 42) |
| `--focus` | oklch(76% 0.2 48) | oklch(58% 0.21 40) |
| `--ok` / `--warn` / `--bad` | LED green / amber / red | darker for light paper |

Every neutral is tinted toward the accent hue. The full token list lives at the
top of `web/src/index.css`; Tailwind's default palette is switched off, so only
these tokens can be used.

## Typography

- Text and headings: IBM Plex Sans (variable), 400–700, roman only. Chosen for readability at small sizes and from a TV, and its industrial character suits the panel look.
- Labels, numbers, logs: JetBrains Mono (variable).
- Both are bundled with Haven (no internet needed).
- Engraved label (`.etch`): mono, 11 px, uppercase, 0.12 em tracking.
- Readouts (`<Readout>`): mono, tabular, with unlit leading zeros.

## Components

- `.plate` panel · `.plate-head` header bar · `.well` recessed area.
- `.key` keycap button (`.key-primary` orange, `.key-danger`, `.key-flat`, `.key-sm`).
- `.led` status light: ok, warn, bad, busy (blinks), off.
- `<Ladder>` segmented meter · `<Trace>` stepped history line.
- `<Sections>` (`.sections`): groups page sections that arrange themselves. One
  section fills the row, two share it, a short last row stretches, and they
  stack on narrow screens. Use it instead of fixed column grids.
- `.board`: the same idea for widget cards, with equal heights per row.
- `<Widget>`: the widget card (plate, header, body). Each has a pencil in its
  header that opens its settings in a dialog.
- `<Modal>` dialog and `<Drawer>` side panel. Editing happens in a dialog with
  Save and Cancel; nothing saves until Save.
- `<Suggest>`: the one search box with a list under it (arrows, Enter, Escape).
  Used for cities and time zones.
- `<Markdown>`: release notes and similar text, built from React elements, never
  raw HTML.
- `.tag` small mono label · `<Notice>` warning well · `<ErrorLine>`.
- Mode selector: keys inside a well; the active key is raised with an orange dot.

## Motion

- Easing `--ease-out` cubic-bezier(0.16, 1, 0.3, 1). Only transform and opacity animate.
- Pages rise in (260 ms); side panels slide in (280 ms); busy LEDs blink.
- Keys press down 1 px. Focus rings appear instantly.
- Reduced motion: no slides, no blinking, opacity only.

## Microinteractions

- Silent success: the console shows results; no "done" pop-ups.
- Pop-ups only for failures and effects you can't see (password changed, TV paired).
- Number keys 1–6 switch pages on desktop, for the pages the role can open.
- Edit controls (a widget's pencil) appear when you point at or tab to the
  widget, and are always visible on touch screens.
- Edit layout: drag a widget to move it; the eye hides or shows it. A hidden
  widget keeps its place as a full-size "hidden" card, so the rows don't shift.

## Backgrounds

Optional animated WebGL backgrounds (Paper Design shaders), chosen separately
for the web app and TV mode, with dim and motion controls. They sit behind the
plates and never carry information.

## TV mode

- Root font scales with the screen (`clamp(16px, 1.25vw, 44px)`), so 1080p and 4K match.
- Arrows move focus spatially; each row remembers its last item; OK selects;
  Back (Escape, Backspace, and Tizen/webOS/Android TV back codes) closes panels.
- Focus is a lit orange bezel plus a slight lift. No hover-only UI.
- Sign-in is by pairing code approved from a signed-in phone.
- Light mode for low-power TVs turns off animation, shaders and heavy effects.
- Soft sounds on move, select and back; they can be switched off.

## Rules

- No cards inside cards. No coloured side stripes. No gradient text. No italics in headings.
- Accent covers under 5% of any screen.
- Spacing on the 4 px grid (2 px only for hairline gaps).
- Works from 320 px phones to 4K TVs with no sideways scrolling.
- No layout shift. Loading content gets a placeholder of the same size, a page
  never shows an empty state before its data has arrived, and the footer
  appears once the page has settled.
- Sections choose their own width (see `<Sections>`). No half-empty rows.
