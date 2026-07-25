import { useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';
import {
  KIND_BUILDING, KIND_OUTDOOR, KIND_POINT, KIND_ORDER, KIND_ICO,
  normKind, multiPlan, hasPlans, iconsFor, defaultIconFor,
} from './site_kinds';

// Re-exported so the existing callers keep working now that the palette is per-kind.
export const DEFAULT_BUILDING_ICON = defaultIconFor(KIND_BUILDING);

// AssetWizard is step 1 of putting a new thing on the geographic map. It asks WHAT is being added
// before anything else, because the kind decides the rest: a building has storeys to draw, an
// outdoor area is one open ground surface, and a point asset (a junction, a gate, a pole) has no
// surface at all — its cameras reach it through the appliance assigned to it.
//
// It only collects intent. The caller creates the site + its plans, then drops the marker on the
// map, so this dialog never has to know about the map or the plan surface.
export function AssetWizard({ busy, onCreate, onCancel }) {
  const t = useT();
  const [kind, setKind] = useState(KIND_BUILDING);
  const [name, setName] = useState('');
  // The glyph is only "chosen" once the operator picks one; until then it follows the kind, so
  // switching from Building to Junction doesn't leave an office block on a traffic light.
  const [icon, setIcon] = useState('');
  const [multi, setMulti] = useState(false);
  // Seeded with the two areas nearly every multi-storey building has, so the common case is
  // "accept and go" rather than "type from empty".
  const [areas, setAreas] = useState(() => [t('bld.areaGround'), t('bld.areaFirst')]);

  const glyph = icon || defaultIconFor(kind);
  const askAreas = multiPlan(kind); // only a building can have more than one plan
  const trimmedAreas = areas.map((a) => a.trim()).filter(Boolean);
  const canSave = name.trim().length > 0 && !busy && (!askAreas || !multi || trimmedAreas.length > 0);

  const setArea = (i, v) => setAreas((list) => list.map((a, k) => (k === i ? v : a)));
  const addArea = () => setAreas((list) => list.concat(''));
  const removeArea = (i) => setAreas((list) => list.filter((_, k) => k !== i));

  const pickKind = (k) => { setKind(k); setIcon(''); };

  const submit = () => {
    if (!canSave) return;
    // What each kind starts life with:
    //  - point   : no plans at all — there is no surface to author.
    //  - outdoor : exactly one ground plan.
    //  - building: one plan, or the areas the operator listed.
    let plans = [];
    if (hasPlans(kind)) {
      if (askAreas && multi) plans = trimmedAreas;
      else plans = [kind === KIND_OUTDOOR ? t('bld.areaGrounds') : t('bld.areaMain')];
    }
    onCreate(name.trim(), glyph, normKind(kind), plans);
  };

  return (
    <div className="fd-overlay" role="dialog" aria-label={t('map.addAsset')}>
      <div className="site-dialog bld-wizard">
        <div className="site-dialog-title"><span className="site-dialog-glyph">{glyph}</span> {t('map.addAsset')}</div>

        <div className="site-dialog-field">
          <span>{t('bld.kindQuestion')}</span>
          <div className="bld-choice bld-choice-3" role="radiogroup" aria-label={t('bld.kindQuestion')}>
            {KIND_ORDER.map((k) => (
              <button
                key={k}
                type="button"
                className={`bld-choice-opt${kind === k ? ' active' : ''}`}
                role="radio"
                aria-checked={kind === k}
                onClick={() => pickKind(k)}
              >
                <Ico n={KIND_ICO[k]} sz={15} />
                <span className="bld-choice-t">{t(`bld.kind.${k}`)}</span>
                <span className="bld-choice-s">{t(`bld.kindHint.${k}`)}</span>
              </button>
            ))}
          </div>
        </div>

        <label className="site-dialog-field">
          <span>{t('map.assetName')}</span>
          <input
            type="text"
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t(`bld.namePlaceholder.${normKind(kind)}`)}
            onKeyDown={(e) => { if (e.key === 'Enter' && !(askAreas && multi)) submit(); }}
          />
        </label>

        <div className="site-dialog-field">
          <span>{t('map.assetIcon')}</span>
          <div className="site-icon-grid" role="listbox" aria-label={t('map.assetIcon')}>
            {iconsFor(kind).map((g) => (
              <button key={g} type="button" className={`site-icon${glyph === g ? ' active' : ''}`} onClick={() => setIcon(g)} aria-selected={glyph === g}>{g}</button>
            ))}
          </div>
        </div>

        {askAreas ? (
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
        ) : null}

        {askAreas && multi ? (
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

        <p className="settings-hint bld-nextnote">{t(`bld.nextHint.${normKind(kind)}`)}</p>

        <div className="site-dialog-actions">
          <button type="button" className="quiet" onClick={onCancel} disabled={busy}>{t('map.cancel')}</button>
          <button type="button" onClick={submit} disabled={!canSave}>{t('bld.createAndPlace')}</button>
        </div>
      </div>
    </div>
  );
}

AssetWizard.propTypes = {
  busy: PropTypes.bool,
  onCreate: PropTypes.func,
  onCancel: PropTypes.func,
};

// SiteDialog is the small rename/re-glyph modal for an EXISTING asset — the wizard's questions
// minus the two one-off branches (kind is fixed once created, since plans hang off it; areas are
// managed in the editor). The glyph palette still follows the asset's kind.
export function SiteDialog({ initialName, initialIcon, kind, busy, onSave, onDelete, onCancel }) {
  const t = useT();
  const k = normKind(kind);
  const [name, setName] = useState(initialName || '');
  const [icon, setIcon] = useState(initialIcon || defaultIconFor(k));
  const canSave = name.trim().length > 0 && !busy;
  return (
    <div className="fd-overlay" role="dialog" aria-label={t('map.editAsset')}>
      <div className="site-dialog">
        <div className="site-dialog-title"><span className="site-dialog-glyph">{icon}</span> {t('map.editAsset')}</div>
        <label className="site-dialog-field">
          <span>{t('map.assetName')}</span>
          <input type="text" autoFocus value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter' && canSave) onSave(name.trim(), icon); }} />
        </label>
        <div className="site-dialog-field">
          <span>{t('map.assetIcon')}</span>
          <div className="site-icon-grid" role="listbox" aria-label={t('map.assetIcon')}>
            {iconsFor(k).map((g) => (
              <button key={g} type="button" className={`site-icon${icon === g ? ' active' : ''}`} onClick={() => setIcon(g)} aria-selected={icon === g}>{g}</button>
            ))}
          </div>
        </div>
        <div className="site-dialog-actions">
          {/* Delete sits apart on the left — the destructive action is never adjacent to Save. It
              takes the whole asset with it (its floor plans and camera placements), so it confirms. */}
          {onDelete ? <button type="button" className="danger-text site-dialog-delete" onClick={onDelete} disabled={busy}>{t('map.deleteAsset')}</button> : null}
          <span className="site-dialog-spacer" />
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
  kind: PropTypes.string,
  busy: PropTypes.bool,
  onSave: PropTypes.func,
  onDelete: PropTypes.func,
  onCancel: PropTypes.func,
};

export { KIND_BUILDING, KIND_OUTDOOR, KIND_POINT };
