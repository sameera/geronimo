---
product: Geronimo
phase: 1
last_updated: 2026-11-09
---

# Design System

Reference implementation: [mocks/geronimo-mock.html](mocks/geronimo-mock.html). Its `:root` block holds the same tokens as this page. Change both together.

## Principles

- **Vibrant, not noisy.** One saturated brand color (violet) and one brand gradient (violet → magenta → rose). Color carries meaning: urgency, topic, account. Everything else stays neutral.
- **Light and dark are equal.** Every color is a token with a light and a dark value. Components never hard-code a color.
- **Fast to scan.** Strong type contrast between unread and read. Color dots and avatars let the eye find a group before reading it.
- **Mobile first.** 44px minimum touch target. Thumb-reach actions (tab bar, selection bar, bottom sheets) sit at the bottom.

## Theming

Tokens use the CSS `light-dark()` function. One declaration holds both values. The browser picks one from `color-scheme`.

```css
:root { color-scheme: light dark; }               /* follow the OS */
:root[data-theme="light"] { color-scheme: light; } /* user override */
:root[data-theme="dark"] { color-scheme: dark; }
:root { --bg: light-dark(#f4f2fb, #0d0b1a); }
```

- Default: follow the OS (`prefers-color-scheme`).
- Override: Settings › Appearance sets `data-theme` on `<html>` to `light` or `dark`. "System" removes the attribute. The server renders the saved choice into the `<html>` tag, so there is no flash on load.
- `light-dark()` needs Chrome 123, Safari 17.5, Firefox 120 or later (all 2024).
- Shadows are not colors, so they use a color token (`--shadow-c`) that is near-transparent black in light and deeper black in dark.

## Color tokens

### Surfaces and text

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| `--bg` | `#f4f2fb` | `#0d0b1a` | Page background |
| `--surface` | `#ffffff` | `#17142a` | Cards, rows, bars |
| `--surface-2` | `#f8f6ff` | `#201c38` | Hover, inputs, tracks |
| `--line` | `#e4e0f2` | `#2e2950` | Borders, dividers |
| `--ink` | `#1a1530` | `#efedfb` | Primary text |
| `--mute` | `#5f5a78` | `#a7a2c4` | Secondary text |
| `--inverse` | `#1a1530` | `#efedfb` | Toast, selection bar |
| `--inverse-ink` | `#f4f2fb` | `#1a1530` | Text on inverse |
| `--inverse-link` | `#b9a9ff` | `#4527d6` | Actions on inverse (Undo, Retry) |

### Brand

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| `--brand` | `#5b3df5` | `#9f8cff` | Primary buttons, links, active tab, badges |
| `--brand-ink` | `#ffffff` | `#12083a` | Text on `--brand` |
| `--brand-soft` | `#ece8ff` | `#2a2156` | Info banners, active pills |
| `--brand-soft-ink` | `#4527d6` | `#c9beff` | Text on `--brand-soft` |
| `--hero` | `#5b3df5 → #a630e0 → #d6246a` (135°) | same | Hero card, sync strip, new-mail pill, logo. Always white text. |

### Status

| Token | Light | Dark | Soft bg (light / dark) |
| --- | --- | --- | --- |
| `--good` | `#0b7a45` | `#4ade80` | `#e3f7ec` / `#12301f` |
| `--warn` | `#a85600` | `#fbbf24` | `#fff1db` / `#33260a` |
| `--bad` | `#c42239` | `#ff6b81` | `#ffe6e9` / `#3a1420` |

### Category colors

Used for dots, avatars, and tile icons. Always paired with a text label, never color alone.

| Token | Light | Dark |
| --- | --- | --- |
| `--urg-0` Needs action today | `#e11d48` | `#ff5c7a` |
| `--urg-1` This week | `#d97706` | `#fbbf24` |
| `--urg-2` Whenever | `#0d9488` | `#2dd4bf` |
| `--urg-3` No urgency | `#8a87a3` | `#8f8aad` |
| `--t-work` | `#5b3df5` | `#9f8cff` |
| `--t-finance` | `#16a34a` | `#4ade80` |
| `--t-shopping` | `#db2777` | `#f472b6` |
| `--t-news` | `#d97706` | `#fbbf24` |
| `--t-social` | `#2563eb` | `#60a5fa` |
| `--t-travel` | `#0891b2` | `#22d3ee` |
| `--t-other` | `#8a87a3` | `#8f8aad` |

Sender avatars pick a topic color by hashing the sender name.

### Account chips

Soft tinted chips, not solid fills, so they don't compete with urgency color.

| Token | Light (bg / text) | Dark (bg / text) |
| --- | --- | --- |
| Gmail | `#fde8e6` / `#b3261e` | `#3d1a1a` / `#ff9b91` |
| Outlook | `#e3f0fb` / `#0a5ca3` | `#10283f` / `#8cc4ff` |

### Contrast

Checked against WCAG 2.2. All text pairs are at least 4.5:1. Category dots are at least 3:1 against `--surface`.

| Pair | Light | Dark |
| --- | --- | --- |
| `--ink` on `--surface` | 17.6 | 15.5 |
| `--mute` on `--surface` | 6.5 | 7.4 |
| `--brand-ink` on `--brand` | 6.1 | 6.8 |
| White on `--hero` (weakest stop) | 4.9 | 4.9 |
| Status text on its soft bg | ≥ 4.7 | ≥ 7.2 |
| Account chip text on chip bg | ≥ 5.6 | ≥ 7.6 |

## Typography

System font stack: `system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`. No web font download.

| Token | Size | Weight | Use |
| --- | --- | --- | --- |
| `--fs-xs` | 12px | 600 | Eyebrows (uppercase, +0.06em), chips, meta |
| `--fs-sm` | 13px | 400 | Secondary lines, notes |
| `--fs-md` | 15px | 400 / 600 | Body, row titles |
| `--fs-lg` | 17px | 700 | Top bar titles |
| `--fs-xl` | 24px | 800 | Screen headings, tile numbers |
| `--fs-2xl` | 40px | 800 | Hero number |

- Unread row title: 650 weight, `--ink`. Read row title: 500 weight, `--mute`.
- Numbers use `font-variant-numeric: tabular-nums`.

## Spacing, radius, elevation

- Spacing: 4px base. `--s-1` 4, `--s-2` 8, `--s-3` 12, `--s-4` 16, `--s-5` 24, `--s-6` 32.
- Radius: `--r-sm` 8 (inputs, small buttons), `--r-md` 12 (buttons, avatars), `--r-lg` 16 (cards, tiles), `--r-xl` 24 (sheets), `--r-pill` 999.
- Elevation: `--shadow-1` for cards and tiles. `--shadow-2` for toast, pill, sheets. Dark mode relies more on borders than shadows.
- Focus: `--focus` ring (3px, brand at 45%) on `:focus-visible`. Never remove focus without replacing it.

## Motion

- 150ms ease-out for hover, press, and toggles. 200ms for progress bars.
- Press: buttons scale to 0.98.
- `prefers-reduced-motion: reduce` turns transitions off.

## Components

| Component | Notes |
| --- | --- |
| Top bar | Sticky. Translucent `--surface` with blur. Back button left, title, one action right. |
| Tab bar | Fixed bottom, 4 tabs. Active tab: `--brand` label, icon in a `--brand-soft` pill. |
| Hero card | Home "Needs attention". `--hero` gradient, white text, big count. Taps through to the urgent group. |
| Card | `--surface`, `--line` border, `--r-lg`, `--shadow-1`. Eyebrow `h2`. |
| Tile | View shortcuts on Home. Icon circle tinted with its category color. |
| Group row | Avatar (category color, initial), title, meta line with account chips, unread badge. |
| Email row | Checkbox, urgency dot + sender, subject, account chip, age, star. Unread rows get a 3px `--brand` left edge. |
| Badge | `--brand` pill with count. Zero: `--surface-2` and `--mute`. Urgent: `--urg-0`. |
| Button | Primary `--brand`. Hero CTA uses `--hero`. Danger is text-colored `--bad` with a tinted border. |
| Chip control | Filter and sort pills. Active: `--brand` fill. |
| Segmented control | Theme picker. Active segment raised on `--surface`. |
| Banner | `--warn-soft` for re-auth, `--brand-soft` for info. Action button inside. |
| Progress bar | `--surface-2` track, gradient fill. |
| Selection bar, toast | `--inverse` surface, `--inverse-link` actions. |
| Bottom sheet, dialog | `--surface`, `--r-xl` top corners, grab handle. Backdrop is ink at 55%. |
| Empty state | Large glyph in a tinted circle, heading, one line of help. |

## Applying to the app

`internal/web/static/app.css` carries the same `:root` token block as the mock. Keep the two in sync with this page.

- The app has no theme setting yet, so it follows the OS. The `data-theme` rules are ready for when Settings › Appearance lands.
- `app.css` must not set fixed `width` or `min-width` in px or rem. `TestPreConsentPageFitsPhoneWidth` enforces this.
