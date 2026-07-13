// The embedded node pages are styled by mymatasan's REAL stylesheets, imported here as
// raw strings (via the webpack `?raw` rule) and injected into a Shadow DOM by ShadowScope.
// Because this pulls straight from mymatasan's source, any design change there flows into
// myseliasan's embedded pages on the next build — no re-sync, no drift.
//
// The shared stylesheets must be pulled in the SAME way. Everywhere else they arrive via
// style-loader, which injects a <style> into the main document — and a shadow root does
// not inherit document stylesheets, only inherited custom properties. So anything that
// moved out of mymatasan's app.css and into @shared (the brand mark, the password reveal
// field) would render unstyled inside the embed unless it is concatenated here too.
import appCss from '@mymatasan/styles/app.css?raw';
import rbacCss from '@mymatasan/styles/rbac-standard.css?raw';
import brandCss from '@shared/styles/brand.css?raw';
import passwordCss from '@shared/styles/password.css?raw';

export const mymatasanCss = `${appCss}\n${rbacCss}\n${brandCss}\n${passwordCss}`;
