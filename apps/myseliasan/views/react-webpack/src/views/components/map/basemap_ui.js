import { useRef } from 'react';
import PropTypes from 'prop-types';
import { useT, Ico } from '@shared';

// The two pieces of chrome the offline basemap needs, and nothing else the map does.
//
// myseliasan is an intranet/air-gapped control plane, so there is no tile server to fall back on:
// map data for an area exists only if someone downloaded it onto this box. Both of these exist to
// make that state legible instead of leaving an operator staring at a blank stage wondering
// whether the fleet failed to load.
//
// Extracted from fleet_map.js unchanged. Presentational only — the region list, the layers and the
// download itself stay with the map that owns them.

// BasemapDownloadBanner appears when the view has been panned outside every downloaded region.
// It offers the download when the server can do one, and says why not when it cannot — "no data
// here" with no explanation reads as a bug rather than a setup step.
export function BasemapDownloadBanner({ canDownload, downloading, envManaged, onDownload, onSetUp }) {
  const t = useT();
  return (
    <div className="fleet-map-download">
      <span><Ico n="globe" sz={14} /> {t('map.noDataHere')}</span>
      {canDownload ? (
        <button type="button" onClick={onDownload} disabled={downloading}>
          {downloading ? <><Ico n="reload" sz={13} /> {t('map.downloading')}</> : <><Ico n="download" sz={13} /> {t('map.downloadRegion')}</>}
        </button>
      ) : (
        <>
          <span className="fleet-map-download-note">{t('map.downloadNotConfigured')}</span>
          {/* envManaged: the source came from the environment, so it is the deployment's to change,
              not this screen's — offering a form that cannot win would be a lie. */}
          {!envManaged ? <button type="button" onClick={onSetUp}><Ico n="sliders" sz={13} /> {t('map.setUp')}</button> : null}
        </>
      )}
    </div>
  );
}
BasemapDownloadBanner.propTypes = {
  canDownload: PropTypes.bool, downloading: PropTypes.bool, envManaged: PropTypes.bool,
  onDownload: PropTypes.func, onSetUp: PropTypes.func,
};

// BasemapSetupDialog points the server at a remote PMTiles archive to extract regions from. It
// also reports whether the extraction tool is installed, because a saved source with no tool is a
// setup that looks finished and cannot download anything.
export function BasemapSetupDialog({ config, onSave, onCancel }) {
  const t = useT();
  const inputRef = useRef(null);
  return (
    <div className="fd-overlay" role="dialog" aria-label={t('map.basemapSetup')}>
      <div className="site-dialog">
        <div className="site-dialog-title"><Ico n="globe" sz={16} /> {t('map.basemapSetup')}</div>
        <p className="settings-hint" style={{ margin: 0 }}>{t('map.basemapSetupHint')}</p>
        <label className="site-dialog-field">
          <span>{t('map.sourceUrl')}</span>
          <input ref={inputRef} type="text" defaultValue={config.source || ''} placeholder="https://build.protomaps.com/20260719.pmtiles" />
        </label>
        <div className={`bm-tool-status ${config.hasTool ? 'ok' : 'bad'}`}>
          <Ico n={config.hasTool ? 'check-ok' : 'warning'} sz={13} /> {config.hasTool ? t('map.toolInstalled') : t('map.toolMissing')}
        </div>
        <div className="site-dialog-actions">
          <button type="button" className="quiet" onClick={onCancel}>{t('map.cancel')}</button>
          <button type="button" onClick={() => onSave((inputRef.current && inputRef.current.value.trim()) || '')}>{t('fd.save')}</button>
        </div>
      </div>
    </div>
  );
}
BasemapSetupDialog.propTypes = { config: PropTypes.object, onSave: PropTypes.func, onCancel: PropTypes.func };
