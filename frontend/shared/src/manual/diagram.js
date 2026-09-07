// The manual's drawn figures: a ```flow / ```arch / ```seq / ```spec fence in, inline SVG out.
//
// The grammar is defined and OWNED by domain/shared/manual/diagram in Go, which every app's
// manual test runs over every fence in every language. That parser is strict and rejects anything
// it does not fully understand, so a malformed fence fails the build and never reaches a reader.
// This one is therefore deliberately lenient: by the time a fence gets here it has been validated,
// and the worst thing a renderer can do on an appliance is throw and take the article with it.
//
// If you change the grammar, change it there first.
//
//   step  intake : Badge presented at reader        a node: <shape> <id> [=> link] : <label>
//   ask   known  : Credential known?                everything before the ':' is structure
//   known -> sched : yes                            an edge: <from> -> <to> [: label]
//   known --> log                                   '-->' draws a reply or an aside
//
// Everything before the ':' is language-independent and must be identical in all four
// translations; everything after it is prose. That split is the whole reason a translated diagram
// can be checked at all.
//
// Drawing rules that are not negotiable, because each one is a property the manual promises:
//   - Colour comes from --ui-* tokens only, so a figure is legible in both themes and in every
//     app's palette without this file knowing any of them.
//   - The output is static SVG with no script, so it survives window.print() — printing is a
//     stated feature of this manual, not a nicety.
//   - Latin text inside a figure is not mirrored for Arabic; the LAYOUT is. A port number or a
//     file path must read left-to-right in every language, the same rule the code blocks follow.

const ESC = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, (c) => ESC[c]);

const KINDS = ['flow', 'arch', 'seq', 'spec'];

export const isDiagramKind = (info) => KINDS.includes(String(info || '').trim());

// ---------------------------------------------------------------------------------------------
// Parsing — the same shapes the Go grammar accepts, read the same way.
// ---------------------------------------------------------------------------------------------

const ARROWS = { '->': false, '-->': true };

// readQuoted pulls a `backticked value` off the token stream; it may span tokens so a value can
// contain spaces.
function readQuoted(fields, start) {
  const first = fields[start];
  if (first.length > 1 && first.endsWith('`')) {
    return { value: first.slice(1, -1), next: start };
  }
  let joined = first.slice(1);
  for (let i = start + 1; i < fields.length; i += 1) {
    const tail = fields[i].endsWith('`') ? fields[i].slice(0, -1) : fields[i];
    joined = joined ? `${joined} ${tail}` : tail;
    if (fields[i].endsWith('`')) return { value: joined, next: i };
  }
  return { value: joined, next: fields.length - 1 };
}

// parseDiagram turns a fence body into { kind, title, nodes, edges }. It never throws: an
// unreadable line is skipped, because half a figure beside the prose beats a blank article.
export function parseDiagram(kind, body) {
  const out = { kind, title: '', nodes: [], edges: [] };
  const seen = new Set();

  for (const raw of String(body || '').replace(/\r\n/g, '\n').split('\n')) {
    const line = raw.trim();
    if (!line || line.startsWith('#')) continue;

    const colon = line.indexOf(':');
    const head = colon >= 0 ? line.slice(0, colon) : line;
    const label = colon >= 0 ? line.slice(colon + 1).trim() : '';
    const fields = head.split(/\s+/).filter(Boolean);

    if (fields[0] === 'title') {
      out.title = label;
      continue;
    }

    // An edge is recognised by an arrow standing alone between two ids. The label is never
    // scanned, so prose containing "->" is not mistaken for an arrow.
    const arrowAt = fields.findIndex((f) => f in ARROWS);
    if (arrowAt === 1 && fields.length === 3) {
      out.edges.push({ from: fields[0], to: fields[2], dashed: ARROWS[fields[1]], label });
      continue;
    }

    if (fields.length < 2 || !label) continue;
    const node = { shape: fields[0], id: fields[1], label, link: '', value: '' };
    for (let i = 2; i < fields.length; i += 1) {
      if (fields[i] === '=>' && i + 1 < fields.length) {
        node.link = fields[i + 1];
        i += 1;
      } else if (fields[i].startsWith('`')) {
        const got = readQuoted(fields, i);
        node.value = got.value;
        i = got.next;
      }
    }
    if (seen.has(node.id)) continue;
    seen.add(node.id);
    out.nodes.push(node);
  }
  return out;
}

// ---------------------------------------------------------------------------------------------
// Text measurement and wrapping
// ---------------------------------------------------------------------------------------------

const FONT = 12.5;
const LINE_H = 15;

// charWidth approximates a glyph's advance without touching the DOM, because measuring text
// would mean laying the figure out twice and would not work at all during print or SSR.
// CJK is full-width; Arabic sits between the two. Being a little generous is the safe direction —
// a box slightly too wide looks considered, a box slightly too narrow clips its label.
function charWidth(ch) {
  const c = ch.codePointAt(0);
  if (c >= 0x2e80 && c <= 0xa4cf) return FONT;         // CJK
  if (c >= 0xac00 && c <= 0xd7a3) return FONT;         // Hangul
  if (c >= 0xff00 && c <= 0xff60) return FONT;         // full-width forms
  if (c >= 0x0600 && c <= 0x06ff) return FONT * 0.52;  // Arabic
  return FONT * 0.53;
}

const textWidth = (s) => Array.from(String(s)).reduce((w, ch) => w + charWidth(ch), 0);

// wrap breaks a label to fit maxWidth, over at most maxLines. CJK has no spaces, so it falls back
// to breaking between characters rather than overflowing the box.
function wrap(text, maxWidth, maxLines) {
  const words = String(text).split(/\s+/).filter(Boolean);
  const lines = [];
  let line = '';

  const pushChar = (word) => {
    for (const ch of Array.from(word)) {
      if (textWidth(line + ch) > maxWidth && line) {
        lines.push(line);
        line = '';
      }
      line += ch;
    }
  };

  for (const word of words) {
    const candidate = line ? `${line} ${word}` : word;
    if (textWidth(candidate) <= maxWidth) {
      line = candidate;
      continue;
    }
    if (line) lines.push(line);
    line = '';
    if (textWidth(word) <= maxWidth) line = word;
    else pushChar(word);
  }
  if (line) lines.push(line);

  if (lines.length > maxLines) {
    const kept = lines.slice(0, maxLines);
    kept[maxLines - 1] = `${kept[maxLines - 1].replace(/\s+\S*$/, '')}…`;
    return kept;
  }
  return lines.length ? lines : [''];
}

// ---------------------------------------------------------------------------------------------
// Shared drawing helpers
// ---------------------------------------------------------------------------------------------

// TONES maps a shape to the meaning it carries. `ok` and `end` are an outcome's verdict, `alert`
// is the outcome that is not a verdict — the duress unlock, the suppressed alarm — and it earns
// the one warm colour in the palette precisely because it is rare.
const TONES = {
  step: 'plain', box: 'plain', actor: 'plain', ask: 'ask',
  store: 'store', ext: 'ext', ok: 'ok', end: 'deny', alert: 'alert',
};

function nodeAttrs(node) {
  const tone = TONES[node.shape] || 'plain';
  let attrs = ` class="mdia-node mdia-${tone}${node.link ? ' mdia-linked' : ''}"`;
  if (node.link) {
    const [slug, anchor] = node.link.split('#');
    attrs += ` data-manual="${esc(slug)}"`;
    if (anchor) attrs += ` data-manual-anchor="${esc(anchor)}"`;
    attrs += ' tabindex="0" role="link"';
  }
  return attrs;
}

// label emits centred lines inside a shape.
function label(lines, cx, cy) {
  // +4.5 shifts the run of BASELINES so the block of text is optically centred in the shape,
  // rather than sitting low by half a cap height.
  const top = cy - ((lines.length - 1) * LINE_H) / 2 + 4.5;
  return lines
    .map((l, i) => `<text class="mdia-label" x="${cx}" y="${(top + i * LINE_H).toFixed(1)}">${esc(l)}</text>`)
    .join('');
}

// edgeLabel sits on the arrow with a plate behind it, because an unplated word on a line is
// unreadable exactly where it matters most — at a branch.
//
// A label on a HORIZONTAL arrow is lifted clear of it instead. Sitting on the line would make the
// gap between two boxes have to be as long as the phrase in it, which is what turned a five-stage
// pipeline into a diagram three screens wide.
function edgeLabel(text, x, y, horizontal) {
  if (!text) return '';
  // A horizontal label is placed by the CALLER, clear of the boxes it runs between, so it needs
  // no room in the gap and a long phrase cannot stretch the diagram.
  if (horizontal) {
    return `<text class="mdia-msg" x="${x.toFixed(1)}" y="${y.toFixed(1)}">${esc(text)}</text>`;
  }
  const w = textWidth(text) + 10;
  return `<g class="mdia-elabel">`
    + `<rect x="${(x - w / 2).toFixed(1)}" y="${(y - 8).toFixed(1)}" width="${w.toFixed(1)}" height="16" rx="3" />`
    + `<text x="${x.toFixed(1)}" y="${(y + 3.5).toFixed(1)}">${esc(text)}</text></g>`;
}

// ---------------------------------------------------------------------------------------------
// Ranking — the layered layout both flow and arch are drawn on
// ---------------------------------------------------------------------------------------------

// rankNodes assigns every node a depth: one more than the deepest thing that reaches it.
//
// Edges that would close a cycle are excluded from the ranking and reported back as back-edges,
// so an architecture where a node dials its parent AND the parent forwards to the node still
// lays out sensibly instead of failing to terminate. Order within a rank is DECLARATION order,
// which is the author's only layout control and is documented as such — an author who wants the
// main path on the left declares it first.
function rankNodes(nodes, edges) {
  const index = new Map(nodes.map((n, i) => [n.id, i]));
  const outgoing = new Map(nodes.map((n) => [n.id, []]));
  for (const e of edges) {
    if (index.has(e.from) && index.has(e.to)) outgoing.get(e.from).push(e);
  }

  // Depth-first walk marking edges whose target is already on the stack: those are the ones
  // closing a loop.
  const back = new Set();
  const state = new Map(); // id -> 1 visiting, 2 done
  const visit = (id) => {
    state.set(id, 1);
    for (const e of outgoing.get(id) || []) {
      const s = state.get(e.to);
      if (s === 1) back.add(e);
      else if (s !== 2) visit(e.to);
    }
    state.set(id, 2);
  };
  for (const n of nodes) if (!state.has(n.id)) visit(n.id);

  const forward = edges.filter((e) => !back.has(e) && index.has(e.from) && index.has(e.to));
  const rank = new Map(nodes.map((n) => [n.id, 0]));
  // Longest path: relax until stable. The corpus is a handful of nodes, so the simple form is
  // both fast enough and obviously correct.
  for (let pass = 0; pass < nodes.length; pass += 1) {
    let moved = false;
    for (const e of forward) {
      const want = rank.get(e.from) + 1;
      if (want > rank.get(e.to)) {
        rank.set(e.to, want);
        moved = true;
      }
    }
    if (!moved) break;
  }

  const rows = [];
  for (const n of nodes) {
    const r = rank.get(n.id);
    (rows[r] = rows[r] || []).push(n);
  }
  return { rows: rows.filter(Boolean), rank, back };
}

// ---------------------------------------------------------------------------------------------
// flow / arch
// ---------------------------------------------------------------------------------------------

const PAD = 14;
const H_GAP = 26;
const V_GAP = 34;

// place computes every box's geometry. `axis` is the direction ranks advance in: flow runs down
// the page the way a procedure is read, arch runs across it the way a data path is read.
function place(diagram, axis) {
  const { rows, rank, back } = rankNodes(diagram.nodes, diagram.edges);

  // An edge that jumps over a rank cannot be drawn straight: in a left-to-right arch every box
  // between its ends is directly in the way, and a line crossing a component reads as touching
  // it. Those are bowed over the top instead, which needs a strip of clear space up there.
  const skips = diagram.edges.some((e) => !back.has(e)
    && rank.has(e.from) && rank.has(e.to)
    && Math.abs(rank.get(e.to) - rank.get(e.from)) > 1);

  // An arch's ranks advance across the page, so its arrows are horizontal and its labels sit
  // ABOVE them rather than on them (see edgeLabel). The gap therefore only has to be long enough
  // for an arrow to read as an arrow, not long enough to hold a phrase.
  const gapAlong = axis === 'x' ? 52 : V_GAP;
  // An arch's edge labels sit above the boxes, so the drawing needs a strip of clear space at
  // the top that a flow — whose labels sit between the ranks — does not.
  const headroom = axis === 'x'
    ? (diagram.edges.some((e) => e.label) ? 18 : 0) + (skips ? 40 : 0)
    : 0;

  // One text budget per SHAPE FAMILY keeps boxes on a shared grid. Rectangles and diamonds are
  // sized separately because a diamond only offers text its middle band — roughly 60% of its
  // width — so sizing both from one number either wastes space on the rectangles or truncates
  // the diamonds. It truncated them: "Does a rule judge it worth an alert?" shipped as "Does a
  // rule judge it…", which loses the question the figure exists to ask.
  const widthOf = (pred, fallback) => {
    const labels = diagram.nodes.filter(pred).map((n) => textWidth(n.label));
    return labels.length ? Math.max(...labels) : fallback;
  };
  const isDiamond = (n) => n.shape === 'ask';
  const boxW = Math.min(250, Math.max(150, Math.ceil(widthOf((n) => !isDiamond(n), 90) / 1.7)));
  const askW = Math.min(300, Math.max(
    Math.round(boxW * 1.24),
    Math.ceil(widthOf(isDiamond, 90) / 1.55),
  ));

  const boxes = new Map();
  for (const n of diagram.nodes) {
    const isAsk = n.shape === 'ask';
    const w = isAsk ? askW : boxW;
    // Three lines in a diamond, four in a box: the last resort before a label is truncated, and
    // a taller shape is always better than a lost word.
    const lines = wrap(n.label, isAsk ? w * 0.6 : w - PAD * 2, isAsk ? 3 : 4);
    const h = isAsk
      ? Math.max(72, lines.length * LINE_H + 46)
      : Math.max(40, lines.length * LINE_H + 18);
    boxes.set(n.id, { node: n, w, h, lines, isAsk });
  }

  // Lay each rank out as a centred band, then centre the bands against the widest one.
  const bandSize = rows.map((row) => row.reduce((sum, n, i) => sum
    + (axis === 'y' ? boxes.get(n.id).w : boxes.get(n.id).h)
    + (i ? H_GAP : 0), 0));
  const span = Math.max(...bandSize);
  let along = PAD;
  const top = PAD + headroom;

  rows.forEach((row, r) => {
    const depth = Math.max(...row.map((n) => (axis === 'y' ? boxes.get(n.id).h : boxes.get(n.id).w)));
    let across = top + (span - bandSize[r]) / 2;
    for (const n of row) {
      const b = boxes.get(n.id);
      if (axis === 'y') {
        b.cx = across + b.w / 2;
        b.cy = along + depth / 2;
        across += b.w + H_GAP;
      } else {
        b.cy = across + b.h / 2;
        b.cx = along + depth / 2;
        across += b.h + H_GAP;
      }
    }
    along += depth + (r < rows.length - 1 ? gapAlong : 0);
  });

  const width = axis === 'y' ? span + PAD * 2 : along + PAD;
  const height = axis === 'y' ? along + PAD : span + top + PAD;
  return { boxes, rank, back, width, height };
}

// anchorOn returns the point on a box's edge facing a target, so an arrow meets a shape square
// rather than disappearing under it. Diamonds use their vertices, which is what makes a decision
// read as a decision.
function anchorOn(b, tx, ty) {
  const dx = tx - b.cx;
  const dy = ty - b.cy;
  const horizontal = Math.abs(dx) * b.h > Math.abs(dy) * b.w;
  if (b.isAsk) {
    if (horizontal) return { x: b.cx + Math.sign(dx) * (b.w / 2), y: b.cy };
    return { x: b.cx, y: b.cy + Math.sign(dy) * (b.h / 2) };
  }
  if (horizontal) return { x: b.cx + Math.sign(dx) * (b.w / 2), y: b.cy + (dy / Math.abs(dx || 1)) * 4 };
  return { x: b.cx + (dx / Math.abs(dy || 1)) * 4, y: b.cy + Math.sign(dy) * (b.h / 2) };
}

function drawShape(b) {
  const { node, w, h, cx, cy } = b;
  const x = (cx - w / 2).toFixed(1);
  const y = (cy - h / 2).toFixed(1);
  if (b.isAsk) {
    const pts = [
      `${cx.toFixed(1)},${(cy - h / 2).toFixed(1)}`,
      `${(cx + w / 2).toFixed(1)},${cy.toFixed(1)}`,
      `${cx.toFixed(1)},${(cy + h / 2).toFixed(1)}`,
      `${(cx - w / 2).toFixed(1)},${cy.toFixed(1)}`,
    ].join(' ');
    return `<polygon class="mdia-shape" points="${pts}" />`;
  }
  if (node.shape === 'store') {
    // A cylinder: the one shape that says "this is where it comes to rest".
    const r = 7;
    const top = cy - h / 2;
    const bot = cy + h / 2;
    return `<path class="mdia-shape" d="M ${x} ${(top + r).toFixed(1)}`
      + ` a ${w / 2} ${r} 0 0 1 ${w} 0 v ${(h - r * 2).toFixed(1)}`
      + ` a ${w / 2} ${r} 0 0 1 ${-w} 0 z" />`
      + `<path class="mdia-shape-lid" d="M ${x} ${(top + r).toFixed(1)} a ${w / 2} ${r} 0 0 0 ${w} 0" />`;
  }
  const rx = node.shape === 'ext' || node.shape === 'actor' ? Math.min(16, h / 2) : 4;
  return `<rect class="mdia-shape" x="${x}" y="${y}" width="${w}" height="${h}" rx="${rx}" />`;
}

function drawLayered(diagram, axis, rtl) {
  const placed = place(diagram, axis);
  const { boxes, rank, back } = placed;
  // A loop is routed clear of the ranks it spans, which needs room outside them — sideways for a
  // flow that runs down the page, underneath for an arch that runs across it.
  const detour = back.size ? 36 : 0;
  const width = placed.width + (axis === 'y' ? detour : 0);
  const height = placed.height + (axis === 'x' ? detour : 0);
  const mx = (x) => (rtl ? width - x : x);

  let svg = '';
  for (const e of diagram.edges) {
    const a = boxes.get(e.from);
    const b = boxes.get(e.to);
    if (!a || !b) continue;

    const from = anchorOn(a, b.cx, b.cy);
    const to = anchorOn(b, a.cx, a.cy);
    const cls = `mdia-edge${e.dashed ? ' mdia-dashed' : ''}${back.has(e) ? ' mdia-back' : ''}`;

    if (back.has(e)) {
      // The detour runs PERPENDICULAR to the ranks. Getting that backwards is not a cosmetic
      // slip: in an arch, two boxes in a loop sit on the same horizontal band, so a sideways
      // detour is a straight line drawn through every box between them.
      if (axis === 'x') {
        const under = Math.max(a.cy + a.h / 2, b.cy + b.h / 2) + 18;
        const ax = mx(a.cx);
        const bx = mx(b.cx);
        svg += `<path class="${cls}" d="M ${ax.toFixed(1)} ${(a.cy + a.h / 2).toFixed(1)}`
          + ` L ${ax.toFixed(1)} ${under.toFixed(1)}`
          + ` L ${bx.toFixed(1)} ${under.toFixed(1)}`
          + ` L ${bx.toFixed(1)} ${(b.cy + b.h / 2).toFixed(1)}" marker-end="url(#mdia-arrow)" />`;
        svg += edgeLabel(e.label, (ax + bx) / 2, under + 12, true);
        continue;
      }
      const side = Math.max(a.cx + a.w / 2, b.cx + b.w / 2) + 18;
      svg += `<path class="${cls}" d="M ${mx(a.cx + a.w / 2).toFixed(1)} ${a.cy.toFixed(1)}`
        + ` L ${mx(side).toFixed(1)} ${a.cy.toFixed(1)}`
        + ` L ${mx(side).toFixed(1)} ${b.cy.toFixed(1)}`
        + ` L ${mx(b.cx + b.w / 2).toFixed(1)} ${b.cy.toFixed(1)}" marker-end="url(#mdia-arrow)" />`;
      svg += edgeLabel(e.label, mx(side), (a.cy + b.cy) / 2, false);
      continue;
    }

    svg += `<line class="${cls}" x1="${mx(from.x).toFixed(1)}" y1="${from.y.toFixed(1)}"`
      + ` x2="${mx(to.x).toFixed(1)}" y2="${to.y.toFixed(1)}" marker-end="url(#mdia-arrow)" />`;
    // A rank-skipping edge is bowed clear of the boxes it passes. Only arch needs it: a flow's
    // branches fan out sideways rather than running over their own ranks.
    if (axis === 'x' && Math.abs(rank.get(e.to) - rank.get(e.from)) > 1) {
      const topY = Math.min(a.cy - a.h / 2, b.cy - b.h / 2);
      const x1 = mx(a.cx);
      const x2 = mx(b.cx);
      svg += `<path class="${cls}" d="M ${x1.toFixed(1)} ${topY.toFixed(1)}`
        + ` Q ${((x1 + x2) / 2).toFixed(1)} ${(topY - 56).toFixed(1)}`
        + ` ${x2.toFixed(1)} ${topY.toFixed(1)}" marker-end="url(#mdia-arrow)" />`;
      svg += edgeLabel(e.label, (x1 + x2) / 2, topY - 33, true);
      continue;
    }

    // Lifting a label clear of the boxes only makes sense along the rank axis, where an edge
    // really does run between two boxes on one line. A flow's branch edges are DIAGONAL — they
    // leave a diamond and drop to a box beside it — and lifting one of those puts "yes" above
    // the question instead of on the arrow answering it.
    const flat = axis === 'x' && Math.abs(to.x - from.x) > Math.abs(to.y - from.y) * 1.6;
    const labelY = flat
      ? Math.min(a.cy - a.h / 2, b.cy - b.h / 2) - 5
      : (from.y + to.y) / 2;
    svg += edgeLabel(e.label, mx((from.x + to.x) / 2), labelY, flat);
  }

  for (const b of boxes.values()) {
    const drawn = { ...b, cx: mx(b.cx) };
    svg += `<g${nodeAttrs(b.node)}>${drawShape(drawn)}${label(b.lines, drawn.cx, drawn.cy)}</g>`;
  }
  return { svg, width, height };
}

// ---------------------------------------------------------------------------------------------
// seq
// ---------------------------------------------------------------------------------------------

const SEQ_ROW = 46;
const SEQ_HEAD = 44;

function drawSequence(diagram, rtl) {
  const actors = diagram.nodes;
  const messages = diagram.edges.filter((e) => actors.some((a) => a.id === e.from) && actors.some((a) => a.id === e.to));

  // The columns are sized from the MESSAGES, not just the actor names.
  //
  // Sizing on the actors alone is the obvious reading and it is wrong: "This appliance" is two
  // words while "adopts, using the claim code you generated" is eight, and in a sequence diagram
  // that is the normal ratio, not an edge case. Doing it the obvious way truncated every message
  // on the page to "adopts, using the claim code…" — which loses precisely the step the figure
  // exists to show.
  const widestActor = Math.max(...actors.map((a) => textWidth(a.label)), 90);
  const widestMsg = Math.max(0, ...messages.map((e) => textWidth(e.label || '')));
  const colW = Math.min(320, Math.max(
    130,
    Math.ceil(widestActor / 1.5) + PAD * 2,
    Math.ceil(widestMsg / 2) + 40,
  ));

  // Rows are then sized from what the labels actually wrapped to, so a three-line message pushes
  // the next arrow down instead of overprinting it.
  const wrapped = messages.map((e) => (e.label ? wrap(e.label, colW - 16, 3) : []));
  const rowH = Math.max(SEQ_ROW, ...wrapped.map((l) => l.length * LINE_H + 26));

  const width = PAD * 2 + colW * actors.length;
  const height = SEQ_HEAD + PAD + rowH * (messages.length + 0.5);
  const mx = (x) => (rtl ? width - x : x);
  const centre = new Map(actors.map((a, i) => [a.id, PAD + colW * i + colW / 2]));

  let svg = '';
  // Lifelines first, so every arrow sits on top of them.
  for (const a of actors) {
    svg += `<line class="mdia-lifeline" x1="${mx(centre.get(a.id)).toFixed(1)}" y1="${SEQ_HEAD}"`
      + ` x2="${mx(centre.get(a.id)).toFixed(1)}" y2="${(height - PAD).toFixed(1)}" />`;
  }

  messages.forEach((e, i) => {
    const y = SEQ_HEAD + rowH * (i + 0.7);
    const from = mx(centre.get(e.from));
    const to = mx(centre.get(e.to));
    const dir = Math.sign(to - from) || 1;
    svg += `<line class="mdia-edge${e.dashed ? ' mdia-dashed' : ''}" x1="${from.toFixed(1)}" y1="${y.toFixed(1)}"`
      + ` x2="${(to - dir * 5).toFixed(1)}" y2="${y.toFixed(1)}" marker-end="url(#mdia-arrow)" />`;
    const lines = wrapped[i];
    if (lines.length) {
      const top = y - 8 - (lines.length - 1) * LINE_H;
      svg += lines.map((l, n) => `<text class="mdia-msg" x="${((from + to) / 2).toFixed(1)}"`
        + ` y="${(top + n * LINE_H).toFixed(1)}">${esc(l)}</text>`).join('');
    }
  });

  // Actor heads last so they sit above the lifelines they own.
  for (const a of actors) {
    const cx = mx(centre.get(a.id));
    const lines = wrap(a.label, colW - PAD * 2 - 12, 2);
    const h = Math.max(30, lines.length * LINE_H + 14);
    const w = colW - H_GAP;
    svg += `<g${nodeAttrs(a)}>`
      + `<rect class="mdia-shape" x="${(cx - w / 2).toFixed(1)}" y="${(SEQ_HEAD - h - 8).toFixed(1)}"`
      + ` width="${w.toFixed(1)}" height="${h.toFixed(1)}" rx="4" />`
      + label(lines, cx, SEQ_HEAD - h / 2 - 8) + '</g>';
  }
  return { svg, width, height };
}

// ---------------------------------------------------------------------------------------------
// spec — a table, not a drawing
// ---------------------------------------------------------------------------------------------

// renderSpec emits the manual's own table markup rather than SVG, because a reference table is
// text: it must reflow on a phone, be selectable, be copyable and be searchable by the browser.
// Drawing it would take all four of those away for no gain.
//
// The value is locked to LTR. A port, a path or a protocol reads left-to-right in Arabic exactly
// as a command in a code block does.
function renderSpec(diagram, t) {
  const rows = diagram.nodes.map((n) => `<tr>`
    + `<th scope="row"><code dir="ltr">${esc(n.value)}</code></th>`
    + `<td>${esc(n.label)}</td></tr>`).join('');
  return `<div class="manual-tablewrap"><table class="manual-table mdia-spec">`
    + `<thead><tr><th>${esc(t('manual.diagram.value'))}</th><th>${esc(t('manual.diagram.meaning'))}</th></tr></thead>`
    + `<tbody>${rows}</tbody></table></div>`;
}

// ---------------------------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------------------------

// ARROW is defined once per figure. The id is namespaced but still document-global, which is
// fine: every figure wants the same arrowhead, and a duplicate definition simply wins or loses
// harmlessly.
const ARROW_DEF = '<defs><marker id="mdia-arrow" viewBox="0 0 10 10" refX="9" refY="5"'
  + ' markerWidth="6" markerHeight="6" orient="auto-start-reverse">'
  + '<path d="M 0 0 L 10 5 L 0 10 z" /></marker></defs>';

// renderDiagram turns one fence into the HTML that replaces it in the article.
//
// `t` is the app's translator, used only for the figure's furniture (the spec table's column
// headings). Everything else a reader sees came out of the markdown and is already translated.
export function renderDiagram(kind, body, opts = {}) {
  const t = opts.t || ((k) => k);
  const rtl = !!opts.rtl;

  let diagram;
  try {
    diagram = parseDiagram(kind, body);
  } catch (_) {
    return '';
  }
  if (!diagram.nodes.length) return '';

  const caption = diagram.title
    ? `<figcaption class="mdia-caption">${esc(diagram.title)}</figcaption>`
    : '';

  if (kind === 'spec') {
    return `<figure class="manual-figure-block mdia-figure">${renderSpec(diagram, t)}${caption}</figure>`;
  }

  let drawn;
  if (kind === 'seq') drawn = drawSequence(diagram, rtl);
  else drawn = drawLayered(diagram, kind === 'arch' ? 'x' : 'y', rtl);

  // The accessible name is the figure's own title when it has one. A drawing with no text
  // alternative is invisible to a screen reader, and a manual is the last place that is
  // acceptable — so a figure without a title says what it is instead of saying nothing.
  const name = diagram.title || t(`manual.diagram.${kind}`);

  // The figure is drawn at its NATURAL size and scrolls sideways when it does not fit, rather
  // than being scaled down to the column. Scaling is the tempting default and it is wrong here:
  // a wide architecture diagram squeezed into a text column takes 12.5px labels down to 8px, and
  // an unreadable diagram is worse than one the reader has to nudge. (Print is the exception —
  // there is nowhere to scroll on paper — so the print stylesheet scales it to the page.)
  const w = Math.ceil(drawn.width);
  const h = Math.ceil(drawn.height);
  return `<figure class="manual-figure-block mdia-figure mdia-${kind}">`
    + `<div class="mdia-scroll"><svg class="mdia-svg" role="img" aria-label="${esc(name)}"`
    + ` viewBox="0 0 ${w} ${h}" width="${w}" height="${h}">${ARROW_DEF}${drawn.svg}</svg></div>`
    + `${caption}</figure>`;
}
