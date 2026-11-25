# Physa's design system

Physa draws a product as a living network: capabilities and ideas are tubes, and a tube is as thick as the flow it carries. This system makes that network, and every screen around it, bright, blue and calm. It is built for server-rendered Go templates: plain HTML, the custom properties in `tokens.css` and the classes in `components.css`, no script required. This folder is the only source: the design system artifact is a read-only view of it.

## Content fundamentals

- **Glossary words only.** Capability, idea, strength, lesson, sign-off, Needs you, steward. Never ticket, backlog, feature, mode, inbox, ADR. Chips and kind labels are glossary terms in capitals.
- **Say what happened, then why it is theirs.** "Checkout completion fell while bulk-invite was at 10%" / "you steward it". Title first, reason in the meta line.
- **Verbs on buttons, outcomes not mechanics.** "Sign off", "Accept the lesson", "Turn the flag off". Never "OK" or "Submit".
- **Second person, plain sentences, sentence case.** No exclamation marks, no emoji, no celebration. Empty Needs you says "Nothing needs you."
- **No internal IDs on any screen.** Name things by their words.
- **Strength is honest.** Show flow units with one decimal, the trend in words ("withering in 9 days", "usage unknown"), and the `Experimental` chip until calibration has data.

## Visual foundations

### Color

- Ground is `surface`, a cool near-white blue (deep navy in dark). Panels and rows sit on `surface-raised`; the network view sits in a `surface-sunk` well. Separate with `line`, not shadow.
- Text is `ink` on any surface; secondary text is `ink-muted`. Both hold 4.5:1 on all three surfaces in both themes.
- `accent` is the one interactive blue: links, the primary button (`on-accent` text), the focus ring, the selected tube. `accent-soft` is the hover and selection ground.
- The network has its own blues: `tube-capability` (deepest, the established network), `tube-idea` (brighter, growing), `tube-faded` (decaying, at its own thickness), `tube-ground` (the product's ring). Capability and idea tubes differ in lightness as well as thickness, so they part in grayscale.
- `lesson` is the only warm color in Physa. It marks the knot a withered idea leaves, and nothing else. Text about a lesson uses `lesson-ink`.
- `danger` (orange) is for guardrail trips and rollbacks; `steady` (teal) is for a clear result for an idea. Neither is red or green, and both always travel with a word.

### Type

- Headings and running text in **Inter**: headings at 700 with tight tracking (`display-xl`, `display-l`, `title`), UI and prose at 400 to 600 (`heading`, `body`, `body-s`, `label`). One plain face, so the network carries the character.
- Strength figures, appetites and counts in **JetBrains Mono** (`figure`, `figure-s`), tabular.
- Both are vendored in `fonts/` (Latin, variable, under the SIL Open Font License, with each licence beside its font) and declared in `tokens.css`, so the views work offline and call no one. Keep the fallbacks in `--font-display`, `--font-sans`, `--font-mono`.

### Space, shape, depth

- Spacing steps of 4: `space-1` to `space-8`. The phone gutter is `space-4`; desktop panels pad `space-6`.
- Corners: `radius-s` chips, `radius-m` buttons and rows, `radius-l` panels and the network well. Everything drawn as a tube is round-capped (`radius-tube`).
- One shadow, `shadow-sheet`, for the selected tube's sheet and menus. Nothing else floats.
- Focus is a solid 2px `focus` ring outset 2px, 3:1 or better on every surface.

## The network

The network view is the signature, and every drawing of a tube anywhere follows the same rules (see TubeKey and StrengthMeter):

- **Thickness is strength, by one rule:** `width = 1.5px + 2.0px × √strength`, capped at 22px. Area follows flow. A tapering tube is read where it leaves its parent.
- **One grown body.** The product sits at the centre in `tube-capability`; one translucent body (`tube-idea` at 9%) grows out of it with one organic margin.
- **Capabilities** are tapering veins in `tube-capability`, at fixed angles. **Growing ideas** branch from the capability they extend in `tube-idea` and end in a small webbed fan, their growing front.
- **Fused, never overlapped.** Veins are filled shapes fused by one blur-and-threshold mask, so joins fillet and nothing floats. Paint colour inside the mask.
- **Fading** parts turn `tube-faded` at their own thickness: dimmed, never hidden.
- **Withered ideas** leave a slime trail from their vein out past the margin, ending in a `lesson` ring. The ring stays as long as the lesson does.
- **Established ideas** become veins of their capability, in its colour.
- **Nothing moves** except the zoom when a capability is tapped. Draw nothing that is not a capability, an idea or a lesson.
- **Experimental:** the `Experimental` chip sits by the legend until calibration has data.

## Layout

- Needs you is the home screen: one column of NeedsYouItem rows, most urgent first, with Running now beside it on desktop and below it on a phone. Running now never notifies.
- Every view is designed phone-first for the single-user phone web: rows stack their actions under the title, and sections run in a single column.
- A pursuit's progress is always a RunProgress; a run's quiet sessions say why they are quiet (StreamEvent `--quiet`).
- Respect `prefers-reduced-motion`. Nothing pulses or flows continuously.

## Iconography

Physa has no icon font. Row glyphs are tiny drawings in the network's own vocabulary (a tube, a lesson knot, a ring for a question), drawn inline in SVG with the tube tokens. There is no logo yet: set "Physa" in Inter at 700.

## Components

Each is a set of `px-` classes in `components.css`. Their guidelines are summarised here; the live previews are in `docs/design/components.html`.

- **Button** (`px-btn`, `--primary`, `--quiet`, `--danger`): one verb that names the outcome. At most one primary per row.
- **Chip** (`px-chip` and its kinds): a glossary term in capitals naming what a row is. `--experimental` beside any strength.
- **StrengthMeter** (`px-strength`): a short tube at the tube's own thickness (`--w`), the figure in flow, the trend in words.
- **TubeKey** (`px-key`): the network's legend, every kind of tube drawn with its word.
- **NeedsYouItem** (`px-needs`, `px-row`): one thing waiting on one person, with the one action it needs.
- **CapabilityRow** (`px-cap`): a capability's name, strength and promise, word for word.
- **EmptyState** (`px-empty`): "Nothing needs you."
- **RunProgress** (`px-run`, `--waiting`, `--stalled`, `--spent`): phase, what is running, last activity, tokens against appetite.
- **Calls** (`px-calls`, `px-call`): a run's small choices, each with its reason, checked one by one.
- **Evidence** (`px-evidence`): every piece of evidence, what it added and what it still counts.
- **StreamEvent** (`px-event`, `--record`, `--quiet`): one line of a watched session, written for a person.
