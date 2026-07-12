// The embedded node pages are styled by mymatasan's REAL stylesheets, imported here as
// raw strings (via the webpack `?raw` rule) and injected into a Shadow DOM by ShadowScope.
// Because this pulls straight from mymatasan's source, any design change there flows into
// myseliasan's embedded pages on the next build — no re-sync, no drift.
import appCss from '@mymatasan/styles/app.css?raw';
import rbacCss from '@mymatasan/styles/rbac-standard.css?raw';

export const mymatasanCss = `${appCss}\n${rbacCss}`;
