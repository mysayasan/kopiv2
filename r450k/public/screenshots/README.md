# Product screenshots

Real mymatasan UI captures shown in the **Showcase** section (with the inline SVG
mockups as automatic fallback). Current files:

| File | Shows |
| --- | --- |
| `live-view.webp` | Paged live-view grid with on-frame AI detection boxes |
| `rule-editor.webp` | Two-axis rule editor with a detection zone on the live frame |
| `notifications.webp` | Unified notification feed with snapshots + acknowledge |

Wired up via the `src:` paths in [`../../src/content.js`](../../src/content.js)
(the `showcase.shots` array). Any image that fails to load falls back to its SVG mockup.

## Replacing / adding captures

Keep them **light** — the display area is ~880px wide, so large PNGs are wasteful.
Resize to ~1600px and convert to WebP. With the bundled ffmpeg:

```bash
ffmpeg -i shot.png -vf "scale='min(1600,iw)':-2" -c:v libwebp -quality 80 shot.webp
```

This took the originals from ~5.5 MB total down to ~237 KB. If you use a different
filename or extension, update the matching `src:` in `content.js`.

Capture tips: run mymatasan, set the browser to the dark theme, hide personal data,
and use a 16:10 viewport for consistent framing.
