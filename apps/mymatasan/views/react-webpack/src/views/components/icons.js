// mymatasan now consumes the single shared icon vocabulary (frontend/shared/src/
// icons.js) instead of a hand-copied subset that had already drifted from the RBAC
// apps. Re-exported here so the existing `./icons` import sites keep working
// unchanged; add new glyphs in the shared module, never per-app.
// Import the icons file directly (not the '@shared' barrel) so we don't also pull in
// the shared Toast and its toast.css — mymatasan keeps its own toast styling.
export { Ico, icoSvg } from '@shared/icons';
