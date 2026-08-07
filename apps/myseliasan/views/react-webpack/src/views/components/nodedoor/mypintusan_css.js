// The embedded door-controller pages are styled by mypintusan's REAL stylesheet, imported here
// as a raw string (via the webpack `?raw` rule) and injected — scoped — by scoped_css.js.
// Because this pulls straight from mypintusan's source, any design change there flows into
// myseliasan's embedded pages on the next build: no re-sync, no drift.
//
// mypintusan's frontend keeps everything in ONE stylesheet (src/styles.css) rather than the
// styles/ directory the other node apps use, so this file is shorter than its nodeiot twin.
// Its assets/fonts.css is deliberately NOT pulled in: its @font-face URLs are absolute paths
// into mypintusan's own static tree, which do not exist on this origin — the embed renders in
// myseliasan's font, which is the same family anyway.
//
// The @shared stylesheets the embedded pages depend on must be concatenated here explicitly
// (not left to arrive via the shell's own imports) — the day the shell stops importing one,
// the embed would silently render unstyled. The pages use DataTable (styled by mypintusan's
// own styles.css) and no other shared widget with a stylesheet of its own.
import appCss from '@mypintusan/styles.css?raw';

export const mypintusanCss = [
  appCss,
].join('\n');
