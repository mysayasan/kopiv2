# Drawing figures in the manual

The built-in manual can draw. An article writes a fenced block and the reader renders it as inline
SVG in the app's own colours, in the reader's own language, mirrored for Arabic, and printable.

There are four fences: `flow`, `arch`, `seq` and `spec`.

## The one rule

> Everything before the colon is **structure** and must be byte-identical in all four languages.
> Everything after it is **prose** and must be translated.

Structure is node ids, shapes, edges, link targets and spec values. None of those is language.

That split is not a style preference — it is what makes a translated figure checkable at all. The
build fingerprints each language's copy of every figure and fails if the structures differ, so an
Arabic diagram cannot quietly lose an edge that nobody who reads Arabic will ever look at.

## Writing a node and an edge

```
step  intake : Badge presented at reader        <shape> <id> [=> link] [`value`] : <label>
ask   known  : Credential known?
known -> sched : yes                            <from> -> <to> [: label]
known --> log                                   '-->' is a reply, an aside, or an optional path
title : What this figure shows                  optional, translated, shown as the caption
# anything on a line starting with # is a note to yourself
```

Ids are `[A-Za-z0-9_-]` only. A label may contain anything at all, including `:`, `->` and
backticks — once the first standalone colon is passed, the parser stops looking at syntax.

`=> slug` or `=> slug#anchor` makes the node **clickable**, opening that article. This is the
feature that turns a figure from an illustration into navigation, and it is checked: the target
must resolve, exactly like a contextual `?` button's does.

## Shapes

| Fence  | Shapes | What each one means |
| --- | --- | --- |
| `flow` | `step` | an action |
|        | `ask`  | a decision — drawn as a diamond, and its outgoing edges should be labelled |
|        | `ok`   | the good outcome |
|        | `end`  | a refusal or a stop |
|        | `alert`| an outcome that also raises something (the duress unlock, the suppressed alarm) |
| `arch` | `box`  | a component of this appliance |
|        | `store`| data at rest — drawn as a cylinder |
|        | `ext`  | something outside the box: another appliance, a camera, an operator's browser |
| `seq`  | `actor`| one participant's lifeline |
| `spec` | `row`  | one reference value |

Pick a shape for what the step *is*, not for how you want it to look. `end` is drawn in the
refusal colour because it is a refusal; using it for a neutral step will read as a failure.

## Layout, and the only control you have over it

`flow` runs **down** the page the way a procedure is read. `arch` runs **across** it the way a data
path is read. `seq` places its actors left to right.

Nodes are ranked automatically: a node sits one level deeper than the deepest thing that reaches
it. Within a level, nodes are placed in **declaration order** — that is your layout control. Declare
the main path first and it stays on the left (or on top, in an `arch`).

A figure is drawn at its natural size and scrolls sideways if it does not fit. An `arch` of more
than four or five stages will scroll in a narrow window; that is preferable to shrinking the labels
until nobody can read them. If a figure is scrolling badly, it is usually trying to say two things
and wants to be two figures.

## `spec`, and why its values are checked

```spec
title : Ports a paired node uses
row discovery `49531/udp` : Multicast discovery. Only a control plane holding the same fleet key can see this node.
row control `49532/tcp` : The node dials the parent and keeps the connection open. Mutual TLS.
```

A spec block renders as a table, not a drawing — a reference value must stay selectable, copyable
and findable with the browser's own search. The value is locked to left-to-right in every language,
the same way a code block is.

**Assert every value against the software.** This is not a hypothetical — `mymatasan` does exactly
this for its `control-plane` article's port table, in `apps/mymatasan/manual/manual_test.go`:

```go
manualcheck.SpecValues(t, manual.Library, map[string]string{
    "control-plane/discovery": fmt.Sprintf("%d/udp", pairing.DefaultDiscoveryPort),
    "control-plane/mtls":      fmt.Sprintf("%d/tcp", pairing.DefaultMTLSPort),
    "control-plane/control":   fmt.Sprintf("%d/tcp", pairing.DefaultControlPort),
    "control-plane/media":     fmt.Sprintf("%d/tcp", pairing.DefaultMediaPort),
})
```

`pairing.Default*Port` are the four exported constants in `infra/pairing/packet.go` — gathered
there specifically so a test could name them; they used to be four unexported literals scattered
across as many packages, which is exactly how a manual, a firewall note and the software drift
apart. The key is `<article slug>/<row id>`. A key that no longer resolves fails the build rather
than silently stopping — a check that quietly covers nothing is worse than no check, because it
reads as coverage.

This is not optional ceremony. A confidently wrong port number costs an integrator an afternoon,
where no port number at all costs them a support call. **A reference section without
`SpecValues` behind it makes the product worse than having none.**

## What the build checks

`manualcheck.Library` runs all of this over every fence in every language:

- every fence parses — the Go parser is strict and rejects anything it does not fully understand,
  so a fence the build cannot read never reaches an appliance;
- every language's copy of a figure has the same structure as the English one;
- every `=> slug#anchor` resolves to a real article and a real hand-written anchor;
- `SpecValues` keys resolve, and their values match what the app passed in.

What it cannot check is which word sits on which arrow. A translator who swaps `yes` and `no` will
not be caught here — the fingerprint sees that an edge *has* a label, not what it says. That is the
same limit the anchor check has always had, and the same answer applies: structure is machine
checkable, meaning is what review is for.

## Where the code is

| | |
| --- | --- |
| Grammar, parser, fingerprint (**the authority**) | `domain/shared/manual/diagram` |
| Build guard | `domain/shared/manual/manualcheck/diagrams.go` |
| Renderer | `frontend/shared/src/manual/diagram.js` |
| Styles | `frontend/shared/src/styles/manual.css` (`.mdia-*`) |

The Go parser owns the grammar. The renderer is deliberately lenient because the parser is strict —
by the time a fence reaches a reader, CI has already validated it, and the worst thing a renderer
can do on an appliance is throw and take the article with it. **If you change the grammar, change
it in Go first.**
