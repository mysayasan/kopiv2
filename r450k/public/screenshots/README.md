# Product screenshots

Drop real mymatasan UI captures here to replace the inline SVG mockups in the
**Showcase** section. Expected files (PNG or WebP, ~1280×800, dark theme):

| File | Shows |
| --- | --- |
| `live-view.png` | Paged live-view grid with on-frame AI detection boxes |
| `rule-editor.png` | Two-axis rule editor with a detection zone on the live frame |
| `notifications.png` | Unified notification feed with snapshots + acknowledge |

After adding the files, uncomment the matching `src:` lines in
[`../../src/content.js`](../../src/content.js) (the `showcase.shots` array). Until then,
the styled SVG mockups render automatically, and any image that fails to load falls
back to its mockup.

Capture tips: run mymatasan, set the browser to the dark theme, hide personal data,
and use a 16:10 viewport for consistent framing.
