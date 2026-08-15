# Manual figures

Files dropped in this folder are served by `GET /api/manual/assets/{name}` and can be referenced
from any article with ordinary markdown image syntax:

```markdown
![The node tree](assets/nodes-tree.png)
```

The renderer rewrites an `assets/…` source onto that endpoint, so the same markdown works in the
in-app reader and in the printed output.

Two rules:

- **Keep figures out unless they earn their place.** Every screenshot dates the moment the UI
  moves, and a manual whose pictures disagree with the screen is worse than one with none. Prefer
  prose and inline SVG diagrams; reach for a screenshot only where the layout itself is the thing
  being explained.
- **Nothing loaded from the network.** These files are compiled into the binary, which is what
  keeps the manual working with no outbound connection at all — the posture MySeliaSan is built
  for. An article must never reference an external image URL.

This file exists so the folder is non-empty and therefore embeddable — `//go:embed` cannot match an
empty directory, and it ignores names beginning with `.` or `_`, so a `.gitkeep` would not work.
