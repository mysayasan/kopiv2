import { useEffect, useState } from 'react';
import PropTypes from 'prop-types';
import { useT } from '@shared';
import { PLAN_OBJECTS, boundsOfObject } from './plan_objects';
import { KIND_BUILDING } from '../site_kinds';

// The numeric inspector — the "N-panel".
//
// Every numeric property in this editor used to be a SLIDER: door width, sill, head, rise, aim,
// mount height, steps, bays. There was no way to type 0.90 m, and no way to see where a thing
// actually was. That is the single biggest reason the tool read as a toy rather than as something
// an installer would survey a building with.
//
// So every field here is a TYPED NUMBER FIRST, with a slider beside it where dragging is genuinely
// the nicer gesture. Never a slider alone.
//
// It is driven entirely by the registry (see plan_objects.js): a type declares its `fields` once,
// and this renders them. Adding an object type in P5 gets a working inspector with no edit here,
// which is the whole reason the registry landed before this phase.

// ---- units ---------------------------------------------------------------------------------
//
// The model is split on which side of the metres/pixels line each field sits (see §4 of the plan
// doc): door and window widths are PIXELS, sills and stair heights are METRES. Both are shown in
// metres when the plan has a scale — and, crucially, in pixels with a plain "px" when it does not.
//
// Never print a metre value derived from a scale that was never set. A fabricated number in a
// survey drawing is worse than an honest pixel count, because it looks like an answer.
function toDisplay(field, raw, scale, unit) {
  if (field.type === 'px') {
    if (scale > 0) return { value: raw * scale, unit: 'm', step: 0.01, digits: 2 };
    return { value: raw, unit: 'px', step: 1, digits: 0 };
  }
  if (field.type === 'length') return { value: raw, unit: 'm', step: field.step || 0.05, digits: 2 };
  if (field.type === 'angle') return { value: (raw * 180) / Math.PI, unit: '°', step: 1, digits: 0 };
  return { value: raw, unit: '', step: field.step || 1, digits: 0 };
}
function fromDisplay(field, shown, scale) {
  if (field.type === 'px') return scale > 0 ? shown / scale : shown;
  if (field.type === 'angle') return (shown * Math.PI) / 180;
  return shown;
}
// A `px` field's min/max are expressed in CELLS (ofUnit), because that is how the sliders that
// preceded this were written and it keeps a door's range sensible at any plan scale.
function boundsFor(field, scale, unit) {
  let lo = field.min; let hi = field.max;
  if (field.type === 'px' && field.ofUnit) { lo = (field.min || 0.5) * unit; hi = (field.max || 6) * unit; }
  if (field.type === 'px' && scale > 0) { lo *= scale; hi *= scale; }
  return { lo, hi };
}

// NumField is the control: a typed input, an optional slider, and a unit.
//
// It holds the KEYSTROKES as text while focused, so typing "0." or clearing the box does not fight
// the operator by snapping to a number mid-edit. It commits on blur and on Enter.
function NumField({ label, value, unit, step, digits, min, max, slider, onCommit, onLive, disabled }) {
  const [text, setText] = useState('');
  const [editing, setEditing] = useState(false);
  useEffect(() => { if (!editing) setText(Number.isFinite(value) ? value.toFixed(digits) : ''); }, [value, digits, editing]);
  const commit = () => {
    setEditing(false);
    const n = parseFloat(String(text).replace(',', '.'));
    if (!Number.isFinite(n)) { setText(Number.isFinite(value) ? value.toFixed(digits) : ''); return; }
    const clamped = Math.min(max !== undefined ? max : Infinity, Math.max(min !== undefined ? min : -Infinity, n));
    onCommit(clamped);
  };
  return (
    <label className="pi-field">
      <span className="pi-label">{label}</span>
      <input
        className="pi-num"
        type="text"
        inputMode="decimal"
        value={text}
        disabled={disabled}
        onFocus={() => setEditing(true)}
        onChange={(e) => { setEditing(true); setText(e.target.value); }}
        onBlur={commit}
        onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); commit(); e.target.blur(); } if (e.key === 'Escape') { setEditing(false); setText(Number.isFinite(value) ? value.toFixed(digits) : ''); e.target.blur(); } }}
      />
      {unit ? <em className="pi-unit">{unit}</em> : null}
      {slider && Number.isFinite(min) && Number.isFinite(max) ? (
        <input
          className="pi-slider"
          type="range"
          min={min}
          max={max}
          step={step}
          disabled={disabled}
          value={Number.isFinite(value) ? value : min}
          onChange={(e) => (onLive || onCommit)(+e.target.value)}
          onPointerUp={(e) => onCommit(+e.target.value)}
        />
      ) : null}
    </label>
  );
}
NumField.propTypes = {
  label: PropTypes.string, value: PropTypes.number, unit: PropTypes.string,
  step: PropTypes.number, digits: PropTypes.number, min: PropTypes.number, max: PropTypes.number,
  slider: PropTypes.bool, onCommit: PropTypes.func, onLive: PropTypes.func, disabled: PropTypes.bool,
};

// PlanFields renders one object's transform block plus the fields its type declares.
//
// `onTransform` takes an affine step ({tx,ty} / {px,py,ang} / {px,py,sx,sy,fa}) and is the SAME
// path a drag or a G/R/S transform goes through — so a typed X and a dragged X cannot disagree,
// and both land as one undo step.
export function PlanFields({ typeName, index, obj, scale, unit, siteKind, onPatch, onTransform, onPatchLive }) {
  const t = useT();
  const spec = PLAN_OBJECTS[typeName];
  if (!spec || !obj) return null;
  const box = boundsOfObject(typeName, obj);
  if (!box) return null;

  const m = (px) => (scale > 0 ? px * scale : px);
  const px = (val) => (scale > 0 ? val / scale : val);
  const posUnit = scale > 0 ? 'm' : 'px';
  const posDigits = scale > 0 ? 2 : 0;
  const cx = (box.x1 + box.x2) / 2;
  const cy = (box.y1 + box.y2) / 2;
  const wPx = box.x2 - box.x1;
  const hPx = box.y2 - box.y1;
  // A point has no extent and an opening's size is its own `w` field, so neither offers W/H here —
  // showing a zero-width box would be a control that cannot do anything.
  const resizable = spec.geometry === 'rect' || spec.geometry === 'segment' || spec.geometry === 'polyline';
  const rotatable = spec.geometry === 'rect' || spec.geometry === 'opening';

  return (
    <div className="pi-body">
      <div className="pi-group">
        <div className="pi-group-t">{t('pi.transform')}</div>
        <NumField
          label={t('pi.x')} value={m(cx)} unit={posUnit} digits={posDigits} step={scale > 0 ? 0.01 : 1}
          onCommit={(v) => onTransform({ tx: px(v) - cx, ty: 0 })}
        />
        <NumField
          label={t('pi.y')} value={m(cy)} unit={posUnit} digits={posDigits} step={scale > 0 ? 0.01 : 1}
          onCommit={(v) => onTransform({ tx: 0, ty: px(v) - cy })}
        />
        {rotatable ? (
          <NumField
            label={t('pi.rotation')} value={((obj.a || 0) * 180) / Math.PI} unit="°" digits={0} step={1}
            min={-180} max={180} slider
            onCommit={(v) => onTransform({ px: cx, py: cy, ang: ((v * Math.PI) / 180) - (obj.a || 0) })}
          />
        ) : null}
        {resizable ? (
          <>
            <NumField
              label={t('pi.width')} value={m(wPx)} unit={posUnit} digits={posDigits} step={scale > 0 ? 0.01 : 1}
              onCommit={(v) => { const want = px(v); if (wPx > 0.001 && want > 0) onTransform({ px: cx, py: cy, sx: want / wPx, sy: 1, fa: obj.a || 0 }); }}
            />
            <NumField
              label={t('pi.height')} value={m(hPx)} unit={posUnit} digits={posDigits} step={scale > 0 ? 0.01 : 1}
              onCommit={(v) => { const want = px(v); if (hPx > 0.001 && want > 0) onTransform({ px: cx, py: cy, sx: 1, sy: want / hPx, fa: obj.a || 0 }); }}
            />
          </>
        ) : null}
      </div>

      {spec.fields.length ? (
        <div className="pi-group">
          <div className="pi-group-t">{t(spec.label || 'pi.properties')}</div>
          {spec.fields.map((f) => {
            // A door's hinge and swing flips only mean anything on a building door: a gate is a
            // symmetric barred opening with no hand to set.
            if (f.buildingOnly && siteKind !== KIND_BUILDING) return null;
            const raw = obj[f.key] !== undefined && obj[f.key] !== null ? obj[f.key] : f.default;
            if (f.type === 'bool') {
              return (
                <label key={f.key} className="pi-field pi-check">
                  <span className="pi-label">{t(f.label)}</span>
                  <input type="checkbox" checked={!!raw} onChange={(e) => onPatch({ [f.key]: e.target.checked })} />
                </label>
              );
            }
            if (f.type === 'enum') {
              return (
                <label key={f.key} className="pi-field">
                  <span className="pi-label">{t(f.label)}</span>
                  <select className="pi-select" value={raw} onChange={(e) => onPatch({ [f.key]: e.target.value })}>
                    {f.options.map((o) => <option key={o} value={o}>{t(`pi.opt.${f.key}.${o}`)}</option>)}
                  </select>
                </label>
              );
            }
            const shown = toDisplay(f, Number.isFinite(raw) ? raw : (f.default || 0), scale, unit);
            const b = boundsFor(f, scale, unit);
            return (
              <NumField
                key={f.key}
                label={t(f.label)}
                value={shown.value}
                unit={shown.unit}
                digits={shown.digits}
                step={shown.step}
                min={b.lo}
                max={b.hi}
                slider={f.type !== 'count' || (b.hi - b.lo) <= 100}
                onLive={onPatchLive ? (v) => onPatchLive({ [f.key]: fromDisplay(f, v, scale) }) : undefined}
                onCommit={(v) => onPatch({ [f.key]: fromDisplay(f, v, scale) })}
              />
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

PlanFields.propTypes = {
  typeName: PropTypes.string,
  index: PropTypes.number,
  obj: PropTypes.object,
  scale: PropTypes.number,
  unit: PropTypes.number,
  siteKind: PropTypes.string,
  onPatch: PropTypes.func,
  onPatchLive: PropTypes.func,
  onTransform: PropTypes.func,
};
