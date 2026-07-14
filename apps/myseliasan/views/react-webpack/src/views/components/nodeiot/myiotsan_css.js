// The embedded sensor-hub pages are styled by myiotsan's REAL stylesheets, imported here as
// raw strings (via the webpack `?raw` rule) and injected — scoped — by scoped_css.js.
// Because this pulls straight from myiotsan's source, any design change there flows into
// myseliasan's embedded pages on the next build: no re-sync, no drift.
//
// THE SHARED STYLESHEETS MUST BE PULLED IN THE SAME WAY, and this is the part that has bitten
// us before. Everywhere else they arrive via style-loader, which injects a <style> into the
// main document. That is fine while the embed's host page happens to import the same widget —
// but it is an accident, not a guarantee: the day myseliasan's own shell stops using Tabs or
// the charts, the embedded myiotsan pages that DO use them would silently render unstyled.
// So every @shared stylesheet the embedded components depend on is concatenated here too,
// where the dependency is explicit and survives whatever the shell does.
//
// The order mirrors myiotsan's own App.js (app -> rbac-standard -> iot), because iot.css is
// written to layer on top of the other two.
import appCss from '@myiotsan/styles/app.css?raw';
import rbacCss from '@myiotsan/styles/rbac-standard.css?raw';
import iotCss from '@myiotsan/styles/iot.css?raw';
// @shared stylesheets the embedded pages actually use:
//   charts.css  — ChartCard / LineChart / DonutChart / StatCard (device telemetry, ingest panel)
//   tabs.css    — the device-detail tab bar (Readings / Control / Settings)
//   toast.css   — the toast stack rendered over the embed
//   brand.css   — the shared mark (myiotsan's app.css no longer carries it)
//   password.css— the shared reveal field
// DataTable and SideNav have no stylesheet of their own; their rules live in each app's
// app.css, which is already the first import above.
import chartsCss from '@shared/charts/charts.css?raw';
import tabsCss from '@shared/styles/tabs.css?raw';
import toastCss from '@shared/styles/toast.css?raw';
import brandCss from '@shared/styles/brand.css?raw';
import passwordCss from '@shared/styles/password.css?raw';

export const myiotsanCss = [
  appCss,
  rbacCss,
  iotCss,
  chartsCss,
  tabsCss,
  toastCss,
  brandCss,
  passwordCss,
].join('\n');
