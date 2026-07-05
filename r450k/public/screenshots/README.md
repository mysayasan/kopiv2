# Product screenshots

Real mymatasan UI captures shown in the **Showcase** section (with the inline SVG
mockups as automatic fallback). Current files:

| File | Shows |
| --- | --- |
| `live_views.png` | Live multi-camera grid with on-frame AI detection badges |
| `ai_detection.png` | AI Detection tab: a rule with a detection zone drawn on the real camera frame |
| `teach.png` | Teach — the guided, no-jargon wizard for teaching the camera a new skill (Name it / What kind / Where / Show examples / Check accuracy / Turn it on) |
| `recordings.png` | Recordings page: continuous-NVR day timeline with event-clip markers, plus the clip list |
| `dashboard.png` | Event-analytics dashboard: KPIs, an events-over-time chart, and category/severity donuts |
| `backup_recovery.png` | Backup & Recovery settings: passphrase-protected configuration backup + restore |
| `version_health_language_selection.png` | Version & Health settings shown in Arabic (RTL): versions, update check, service-health tiles |

Wired up via the `src:` paths in each locale's `showcase.shots` array under
[`../../src/content/`](../../src/content/) (`en.js`/`ms.js`/`zh.js`/`ar.js` — every locale
points at the same screenshot files; only `alt`/`tabs`/`lead` text is translated). Any image
that fails to load falls back to its SVG mockup in
[`../../src/sections/Showcase.jsx`](../../src/sections/Showcase.jsx).

## Replacing / adding captures

Keep them **light** — the display area is ~880px wide, so large originals are wasteful.
Resize to 1920px wide (PNG, no re-encode quality loss) with `sharp`:

```bash
npx sharp-cli resize 1920 --input shot.png --output shot.out.png
```

If you use a different filename or extension, update the matching `src:` in every locale
file under `src/content/`.

Capture tips: run mymatasan, set the browser to the dark theme, hide personal data,
and use a 16:10 viewport for consistent framing.
