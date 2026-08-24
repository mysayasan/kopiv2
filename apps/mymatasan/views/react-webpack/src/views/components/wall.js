import { useState, useEffect, useCallback, useRef } from 'react';
import { Ico } from './icons';
import { FormAlert } from './ui';
import { useT } from '@shared/i18n';
import { apiJson } from '../lib/helpers';

// Video walls: the saved, shared arrangements Live View can be pointed at (W3-3b).
//
// Live View already remembered a grid and a set of tiles — in a cookie, which is a
// per-browser preference. A wall is not a preference; it is how a control room is arranged.
// Everything here follows from moving that one thing to the server: a wall can be handed to
// the next shift, opened on a second monitor, and fixed centrally when a camera moves.

// WALL_QUERY_PARAM opens a wall on its own, with no navigation and no chrome — the
// second-monitor mode. It is a URL rather than an in-app mode because the operator has to be
// able to DRAG THE WINDOW to the other screen, and you cannot drag a div.
export const WALL_QUERY_PARAM = 'wall';

// wallFromLocation reads the requested wall id out of the address bar. 0 = the normal app.
export function wallFromLocation() {
  try {
    const raw = new URLSearchParams(window.location.search).get(WALL_QUERY_PARAM);
    const id = Number(raw);
    return Number.isFinite(id) && id > 0 ? id : 0;
  } catch (_) {
    return 0;
  }
}

export async function loadWalls(authHeader) {
  const data = await apiJson('/api/walls', { authHeader });
  return { walls: data?.walls || [], grids: data?.grids || [] };
}

// useWallCycling advances the wall through its pages, and pops an alerting camera onto the
// screen when one fires.
//
// It lives here rather than in the grid because both behaviours are properties of the WALL,
// not of the component rendering it — and because the interaction between them is the whole
// difficulty: a pop that a cycle immediately scrolls past has shown nobody anything.
export function useWallCycling({
  cycleSeconds, autoPopSeconds, pageCount, page, setPage, alertsByCamera, tiles, tilesPerPage,
}) {
  // popUntil is when the current pop expires. While a pop is showing, cycling is SUSPENDED:
  // the point of a pop is that somebody looks at it, and a wall that rotates away after two
  // seconds has done the opposite of raising an alarm.
  const [popUntil, setPopUntil] = useState(0);
  const [popCamera, setPopCamera] = useState(0);
  const returnToRef = useRef(0);
  const seenAlertsRef = useRef(null);
  const pageRef = useRef(page);
  pageRef.current = page;

  // --- pop ------------------------------------------------------------------------
  useEffect(() => {
    if (!autoPopSeconds || !alertsByCamera) return;
    // The FIRST render is a baseline, never a burst of pops: an operator opening the wall
    // would otherwise be dragged through every alert still in the feed.
    const latest = new Map();
    for (const [camId, alerts] of alertsByCamera) {
      const newest = alerts && alerts[0] ? Number(alerts[0].id) : 0;
      if (newest) latest.set(Number(camId), newest);
    }
    if (seenAlertsRef.current === null) {
      seenAlertsRef.current = latest;
      return;
    }
    const seen = seenAlertsRef.current;
    let fired = 0;
    for (const [camId, newest] of latest) {
      if ((seen.get(camId) || 0) < newest) fired = camId;
    }
    seenAlertsRef.current = latest;
    if (!fired) return;

    const index = tiles.findIndex((tile) => Number(tile?.id) === Number(fired));
    if (index < 0) return; // the camera is not on this wall; nothing to pop
    const target = Math.floor(index / Math.max(1, tilesPerPage));
    setPopCamera(fired);
    setPopUntil(Date.now() + autoPopSeconds * 1000);
    if (target !== pageRef.current) {
      // Remember where the wall WAS, so the pop is a visit rather than a permanent move.
      returnToRef.current = pageRef.current;
      setPage(target);
    }
  }, [alertsByCamera, autoPopSeconds, tiles, tilesPerPage, setPage]);

  // --- the pop expiring -------------------------------------------------------------
  useEffect(() => {
    if (!popUntil) return undefined;
    const remaining = Math.max(0, popUntil - Date.now());
    const timer = setTimeout(() => {
      setPopUntil(0);
      setPopCamera(0);
      if (returnToRef.current !== pageRef.current && returnToRef.current < pageCount) {
        setPage(returnToRef.current);
      }
    }, remaining);
    return () => clearTimeout(timer);
  }, [popUntil, pageCount, setPage]);

  // --- cycle --------------------------------------------------------------------------
  useEffect(() => {
    if (!cycleSeconds || pageCount <= 1) return undefined;
    if (popUntil > Date.now()) return undefined; // suspended while a pop is showing
    const timer = setInterval(() => {
      setPage((current) => (current + 1) % pageCount);
    }, cycleSeconds * 1000);
    return () => clearInterval(timer);
    // `page` is a dependency on purpose: paging by hand restarts the dwell, so a manual
    // page does not get whipped away a fraction of a second later.
  }, [cycleSeconds, pageCount, popUntil, page, setPage]);

  return { popCamera: popUntil > Date.now() ? popCamera : 0 };
}

// WallBar is the wall picker and its behaviour controls, shown above the grid.
export function WallBar({
  authHeader, layout, viewTiles, cycleSeconds, autoPopSeconds,
  onApplyWall, onBehaviour, onMessage,
}) {
  const t = useT();
  const [walls, setWalls] = useState([]);
  const [selected, setSelected] = useState(0);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [naming, setNaming] = useState(false);
  const [name, setName] = useState('');
  const appliedRef = useRef(false);

  const refresh = useCallback(async () => {
    try {
      const { walls: rows } = await loadWalls(authHeader);
      setWalls(rows);
      setError('');
      return rows;
    } catch (err) {
      setError(err.message);
      return [];
    }
  }, [authHeader]);

  // On first load, open the default wall if there is one. Once only: re-applying it after
  // every refresh would undo an operator's live rearrangement while they were making it.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const rows = await refresh();
      if (cancelled || appliedRef.current) return;
      const fallback = rows.find((wall) => wall.isDefault);
      if (fallback) {
        appliedRef.current = true;
        setSelected(Number(fallback.id));
        onApplyWall(fallback);
      }
    })();
    return () => { cancelled = true; };
  }, [refresh, onApplyWall]);

  const current = walls.find((wall) => Number(wall.id) === Number(selected)) || null;

  const apply = useCallback((id) => {
    setSelected(Number(id));
    const wall = walls.find((row) => Number(row.id) === Number(id));
    if (wall) {
      appliedRef.current = true;
      onApplyWall(wall);
    }
  }, [walls, onApplyWall]);

  const save = useCallback(async (asNew) => {
    setBusy(true);
    setError('');
    try {
      const cameraIds = viewTiles.map((tile) => Number(tile.id)).filter(Boolean);
      const body = {
        name: asNew ? name : (current ? current.name : name),
        grid: layout,
        cameraIds,
        cycleSeconds: Number(cycleSeconds) || 0,
        autoPopSeconds: Number(autoPopSeconds) || 0,
        // A COPY DOES NOT INHERIT THE DEFAULT. Saving the wall you are looking at under a
        // new name would otherwise take the default with it — and the default is what every
        // other operator's screen opens with, silently changed by somebody making a variant
        // for themselves. Found by the screen pass.
        isDefault: asNew ? false : (current ? !!current.isDefault : false),
      };
      const path = asNew || !current ? '/api/walls' : `/api/walls/${current.id}`;
      const saved = await apiJson(path, { method: 'POST', authHeader, body });
      await refresh();
      setSelected(Number(saved?.id) || 0);
      setNaming(false);
      setName('');
      onMessage?.(t('wall.saved', { name: saved?.name || '' }));
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }, [viewTiles, layout, cycleSeconds, autoPopSeconds, current, name, authHeader, refresh, onMessage, t]);

  const remove = useCallback(async () => {
    if (!current) return;
    setBusy(true);
    try {
      await apiJson(`/api/walls/${current.id}/delete`, { method: 'POST', authHeader });
      setSelected(0);
      await refresh();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }, [current, authHeader, refresh]);

  const openOnAnotherMonitor = useCallback(() => {
    if (!current) return;
    // A real window, because the operator has to be able to drag it to the other screen.
    window.open(
      `${window.location.pathname}?${WALL_QUERY_PARAM}=${current.id}`,
      `wall-${current.id}`,
      'width=1280,height=800',
    );
  }, [current]);

  return (
    <div className="wall-bar">
      {error ? <FormAlert message={error} /> : null}
      <label className="wall-pick">
        <span>{t('wall.wall')}</span>
        <select value={selected} onChange={(e) => apply(e.target.value)} disabled={busy}>
          <option value={0}>{t('wall.unsaved')}</option>
          {walls.map((wall) => (
            <option key={wall.id} value={wall.id}>
              {wall.name}{wall.isDefault ? ` ${t('wall.defaultSuffix')}` : ''}
            </option>
          ))}
        </select>
      </label>

      {/* A wall whose camera has been deleted says so. Rendering one tile fewer without a
          word tells an operator they are watching everything. */}
      {current && current.missingCameras && current.missingCameras.length > 0 ? (
        <span className="wall-missing" title={t('wall.missingHint')}>
          <Ico n="warning" sz={14} /> {t('wall.missing', { n: current.missingCameras.length })}
        </span>
      ) : null}

      {current ? (
        <label className="wall-default">
          <input type="checkbox" checked={!!current.isDefault} disabled={busy}
            onChange={async (e) => {
              setBusy(true);
              try {
                await apiJson(`/api/walls/${current.id}`, {
                  method: 'POST', authHeader,
                  body: {
                    name: current.name, grid: current.grid, cameraIds: current.cameraIds,
                    cycleSeconds: current.cycleSeconds, autoPopSeconds: current.autoPopSeconds,
                    isDefault: e.target.checked,
                  },
                });
                await refresh();
              } catch (err) {
                setError(err.message);
              } finally {
                setBusy(false);
              }
            }} />
          <span>{t('wall.makeDefault')}</span>
        </label>
      ) : null}

      <label className="wall-num">
        <span>{t('wall.cycle')}</span>
        <input type="number" min="0" max="600" value={cycleSeconds}
          onChange={(e) => onBehaviour({ cycleSeconds: Number(e.target.value) || 0, autoPopSeconds })} />
      </label>
      <label className="wall-num">
        <span>{t('wall.autoPop')}</span>
        <input type="number" min="0" max="300" value={autoPopSeconds}
          onChange={(e) => onBehaviour({ cycleSeconds, autoPopSeconds: Number(e.target.value) || 0 })} />
      </label>

      {naming ? (
        <span className="wall-naming">
          <input value={name} onChange={(e) => setName(e.target.value)}
            placeholder={t('wall.namePlaceholder')} aria-label={t('wall.name')} />
          <button type="button" onClick={() => save(true)} disabled={busy || !name.trim()}>
            {t('common.save')}
          </button>
          <button type="button" className="quiet" onClick={() => { setNaming(false); setName(''); }}>
            {t('common.cancel')}
          </button>
        </span>
      ) : (
        <>
          <button type="button" className="quiet" onClick={() => save(false)} disabled={busy || !current}>
            <span className="btn-icon"><Ico n="save" sz={14} /> {t('wall.save')}</span>
          </button>
          <button type="button" className="quiet" onClick={() => setNaming(true)} disabled={busy}>
            <span className="btn-icon"><Ico n="plus" sz={14} /> {t('wall.saveAs')}</span>
          </button>
          <button type="button" className="quiet" onClick={openOnAnotherMonitor} disabled={!current}>
            <span className="btn-icon"><Ico n="copy" sz={14} /> {t('wall.secondMonitor')}</span>
          </button>
          <button type="button" className="quiet danger" onClick={remove} disabled={busy || !current}>
            <span className="btn-icon"><Ico n="trash" sz={14} /> {t('wall.delete')}</span>
          </button>
        </>
      )}
    </div>
  );
}
