import { useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';

// Curated building glyphs an operator can tag a site with; the chosen one is drawn on the geo-map
// marker and shown in pickers. Emoji so it needs no image asset and renders in the OL canvas.
export const BUILDING_ICONS = ['🏢', '🏬', '🏭', '🏠', '🏘️', '🏗️', '🏪', '🏫', '🏥', '🏨', '🏦', '🏛️', '🏟️', '⛪', '🅿️', '⛽', '🗼', '🚧'];
export const DEFAULT_BUILDING_ICON = '🏢';

// BuildingWizard is step 1 of adding a building from the geographic map: what it is called, what
// glyph marks it, and — the branch that shapes everything after — whether it is one space or
// several. A building is a Site; each of its areas is a floor plan under it, so "multiple areas"
// is not a different kind of building, just one that starts with more than one plan to draw.
//
// It only collects intent. The caller creates the site + areas, then drops the marker on the map
// and opens the editor, so this dialog never has to know about the map or the plan surface.
export function BuildingWizard({ busy, onCreate, onCancel }) {
  const t = useT();
  const [name, setName] = useState('');
  const [icon, setIcon] = useState(DEFAULT_BUILDING_ICON);
  const [multi, setMulti] = useState(false);
  // Seeded with the two areas nearly every multi-storey building has, so the common case is
  // "accept and go" rather than "type from empty".
  const [areas, setAreas] = useState(() => [t('bld.areaGround'), t('bld.areaFirst')]);

  const trimmedAreas = areas.map((a) => a.trim()).filter(Boolean);
  const canSave = name.trim().length > 0 && !busy && (!multi || trimmedAreas.length > 0);

  const setArea = (i, v) => setAreas((list) => list.map((a, k) => (k === i ? v : a)));
  const addArea = () => setAreas((list) => list.concat(''));
  const removeArea = (i) => setAreas((list) => list.filter((_, k) => k !== i));

  const submit = () => {
    if (!canSave) return;
    // Single-area buildings still get one plan to draw on — the editor always has a surface, and
    // the building can grow more areas later without a migration.
    onCreate(name.trim(), icon, multi ? trimmedAreas : [t('bld.areaMain')]);
  };

  return (
    <div className="fd-overlay" role="dialog" aria-label={t('map.addBuilding')}>
      <div className="site-dialog bld-wizard">
        <div className="site-dialog-title"><span className="site-dialog-glyph">{icon}</span> {t('map.addBuilding')}</div>

        <label className="site-dialog-field">
          <span>{t('map.buildingName')}</span>
          <input
            type="text"
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('bld.namePlaceholder')}
            onKeyDown={(e) => { if (e.key === 'Enter' && !multi) submit(); }}
          />
        </label>

        <div className="site-dialog-field">
          <span>{t('map.buildingIcon')}</span>
          <div className="site-icon-grid" role="listbox" aria-label={t('map.buildingIcon')}>
            {BUILDING_ICONS.map((g) => (
              <button key={g} type="button" className={`site-icon${icon === g ? ' active' : ''}`} onClick={() => setIcon(g)} aria-selected={icon === g}>{g}</button>
            ))}
          </div>
        </div>

        <div className="site-dialog-field">
          <span>{t('bld.areasQuestion')}</span>
          <div className="bld-choice" role="radiogroup" aria-label={t('bld.areasQuestion')}>
            <button type="button" className={`bld-choice-opt${!multi ? ' active' : ''}`} role="radio" aria-checked={!multi} onClick={() => setMulti(false)}>
              <Ico n="grid2" sz={15} />
              <span className="bld-choice-t">{t('bld.singleArea')}</span>
              <span className="bld-choice-s">{t('bld.singleAreaHint')}</span>
            </button>
            <button type="button" className={`bld-choice-opt${multi ? ' active' : ''}`} role="radio" aria-checked={multi} onClick={() => setMulti(true)}>
              <Ico n="building" sz={15} />
              <span className="bld-choice-t">{t('bld.multiArea')}</span>
              <span className="bld-choice-s">{t('bld.multiAreaHint')}</span>
            </button>
          </div>
        </div>

        {multi ? (
          <div className="site-dialog-field">
            <span>{t('bld.areas')}</span>
            <ul className="bld-arealist">
              {areas.map((a, i) => (
                // Index keys are safe here: rows are a plain ordered list with no state of their
                // own, and reordering is not offered in this step.
                // eslint-disable-next-line react/no-array-index-key
                <li key={i} className="bld-arearow">
                  <Ico n="grid2" sz={12} />
                  <input
                    type="text"
                    value={a}
                    onChange={(e) => setArea(i, e.target.value)}
                    placeholder={t('bld.areaPlaceholder')}
                    onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addArea(); } }}
                  />
                  <button type="button" className="bld-arearow-x" onClick={() => removeArea(i)} disabled={areas.length <= 1} aria-label={t('bld.removeArea')}>
                    <Ico n="x" sz={12} />
                  </button>
                </li>
              ))}
            </ul>
            <button type="button" className="linklike bld-addarea" onClick={addArea}><Ico n="plus" sz={12} /> {t('bld.addArea')}</button>
          </div>
        ) : null}

        <p className="settings-hint bld-nextnote">{t('bld.nextHint')}</p>

        <div className="site-dialog-actions">
          <button type="button" className="quiet" onClick={onCancel} disabled={busy}>{t('map.cancel')}</button>
          <button type="button" onClick={submit} disabled={!canSave}>{t('bld.createAndPlace')}</button>
        </div>
      </div>
    </div>
  );
}

BuildingWizard.propTypes = {
  busy: PropTypes.bool,
  onCreate: PropTypes.func,
  onCancel: PropTypes.func,
};

// SiteDialog is the small rename/re-glyph modal for an EXISTING building — the wizard's questions
// minus the one-off "how many areas" branch (areas are managed in the editor once it exists).
export function SiteDialog({ initialName, initialIcon, busy, onSave, onCancel }) {
  const t = useT();
  const [name, setName] = useState(initialName || '');
  const [icon, setIcon] = useState(initialIcon || DEFAULT_BUILDING_ICON);
  const canSave = name.trim().length > 0 && !busy;
  return (
    <div className="fd-overlay" role="dialog" aria-label={t('map.editBuilding')}>
      <div className="site-dialog">
        <div className="site-dialog-title"><span className="site-dialog-glyph">{icon}</span> {t('map.editBuilding')}</div>
        <label className="site-dialog-field">
          <span>{t('map.buildingName')}</span>
          <input type="text" autoFocus value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter' && canSave) onSave(name.trim(), icon); }} />
        </label>
        <div className="site-dialog-field">
          <span>{t('map.buildingIcon')}</span>
          <div className="site-icon-grid" role="listbox" aria-label={t('map.buildingIcon')}>
            {BUILDING_ICONS.map((g) => (
              <button key={g} type="button" className={`site-icon${icon === g ? ' active' : ''}`} onClick={() => setIcon(g)} aria-selected={icon === g}>{g}</button>
            ))}
          </div>
        </div>
        <div className="site-dialog-actions">
          <button type="button" className="quiet" onClick={onCancel} disabled={busy}>{t('map.cancel')}</button>
          <button type="button" onClick={() => onSave(name.trim(), icon)} disabled={!canSave}>{t('fd.save')}</button>
        </div>
      </div>
    </div>
  );
}

SiteDialog.propTypes = {
  initialName: PropTypes.string,
  initialIcon: PropTypes.string,
  busy: PropTypes.bool,
  onSave: PropTypes.func,
  onCancel: PropTypes.func,
};
