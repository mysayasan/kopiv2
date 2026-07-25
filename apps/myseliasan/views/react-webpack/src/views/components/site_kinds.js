// Site kinds — the frontend half of entities.SiteKind* (apps/myseliasan/entities/site.go).
//
// Not every place a camera watches is a building. A park is one open ground surface with no
// storeys; a traffic-light junction is a pole with cameras on it and no surface at all. One `kind`
// on the site decides all three of: how many plans it can hold, what its map marker looks like, and
// what clicking it opens. Everything that used to assume "site == building" reads this instead.

export const KIND_BUILDING = 'building';
export const KIND_OUTDOOR = 'outdoor';
export const KIND_POINT = 'point';

// normKind mirrors entities.NormalizeSiteKind: anything unrecognised (including the empty string
// on every site that predates the field) is a building.
export function normKind(kind) {
  return kind === KIND_OUTDOOR || kind === KIND_POINT ? kind : KIND_BUILDING;
}

// hasPlans — a point asset owns no floor plans, so there is nothing to drill into or author.
export const hasPlans = (kind) => normKind(kind) !== KIND_POINT;
// multiPlan — only a building has more than one plan. An outdoor area is exactly one ground
// surface, which is why its editor never offers "add area".
export const multiPlan = (kind) => normKind(kind) === KIND_BUILDING;

// Per-kind glyph palettes. Emoji so a marker needs no image asset and renders natively on the
// OpenLayers canvas — the same reason the building palette was emoji to begin with, and it keeps
// the intranet/air-gap rule (no external icon fetch).
export const KIND_ICONS = {
  [KIND_BUILDING]: ['🏢', '🏬', '🏭', '🏠', '🏘️', '🏗️', '🏪', '🏫', '🏥', '🏨', '🏦', '🏛️', '🏟️', '⛪'],
  [KIND_OUTDOOR]: ['🌳', '🏞️', '🅿️', '🏕️', '⚽', '🛝', '⛲', '🌾', '🚜', '⛽', '🏖️', '🧱'],
  [KIND_POINT]: ['🚦', '🚧', '🚏', '🚪', '🛑', '🗼', '📡', '💡', '🎥', '🔌', '⛩️', '🚰'],
};
export const KIND_DEFAULT_ICON = {
  [KIND_BUILDING]: '🏢',
  [KIND_OUTDOOR]: '🌳',
  [KIND_POINT]: '🚦',
};

// The kinds in the order the wizard offers them, with the shared-icon name for each choice tile.
export const KIND_ORDER = [KIND_BUILDING, KIND_OUTDOOR, KIND_POINT];
export const KIND_ICO = {
  [KIND_BUILDING]: 'building',
  [KIND_OUTDOOR]: 'grid2',
  [KIND_POINT]: 'map-pin',
};

export const iconsFor = (kind) => KIND_ICONS[normKind(kind)];
export const defaultIconFor = (kind) => KIND_DEFAULT_ICON[normKind(kind)];
// siteGlyph resolves what to draw for a site: its chosen glyph, else its kind's default.
export const siteGlyph = (site) => (site && site.icon) || defaultIconFor(site && site.kind);
