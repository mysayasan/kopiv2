import { useState, useEffect, useMemo, useCallback } from 'react';
import { Ico } from './icons';
import { useT } from '@shared/i18n';
import { FormBusyOverlay, AccordionList, AccordionItem } from './ui';
import { useSnapshotBlob } from '../hooks';
import { scheduleDayOptions } from '../lib/constants';
import {todayDateString,apiBase,fieldValue,formatTimestamp,parseMetadata,formatPercent,parseBoundingBox,formatSourceLabel,cameraTitle,orderedSavedCameras,parseZonePolygon,defaultZonePolygon,normalizeLineConfig,parseLineRuleConfig,lineRuleConfigText,lineCountFromRule,parseCrowdRuleConfig,parseLPRRuleConfig,lprRuleConfigText,detectionModes,modeFromDetectionType,detectionTypeForMode,targetClassesFromRule,buildRuleConfigForMode,ruleDestinationsFromConfig,applyRuleDestinations,groupedClassOptions,classDisplayName,defaultVisionRuleDraft,weeklySchedulePolicy,rangeSchedulePolicy,schedulePresetPolicy,scheduleDraftFromPolicy,scheduleSummary } from '../lib/helpers';
import { ZoneDrawingPreview, LineDrawingPreview } from './previews';
import { SavedDeviceNav } from './cameras';

// classIsLive reports whether a registry class can actually be detected right
// now: built-ins/groups always show as usable, while a trained class is "live"
// only when the active model produces one of its labels.
function classIsLive(cls, activeModelClasses) {
  if (!cls || cls.source !== 'trained') return true;
  const active = activeModelClasses || [];
  return (cls.memberList || []).some((label) => active.includes(String(label).toLowerCase()));
}

export function VisionTab({
  saved,
  rules,
  alerts,
  classes,
  labelCatalog,
  activeModelClasses,
  destinations,
  ruleDraft,
  busy,
  authHeader,
  streamConfig,
  onRuleDraft,
  onSaveRule,
  onSaveClass,
  onDeleteClass,
  onEditRule,
  onDeleteRule,
  onTriggerTestAlert,
  onAcknowledgeAlert,
  onPrepareCamera,
  onReload,
}) {
  const t = useT();
  const orderedSaved = useMemo(() => orderedSavedCameras(saved), [saved]);
  const selectedCameraId = Number(ruleDraft.cameraId) || Number(orderedSaved[0]?.id) || 0;
  const selectedCamera = saved.find((device) => Number(device.id) === selectedCameraId) || orderedSaved[0] || null;
  const selectedRules = selectedCamera
    ? rules.filter((rule) => Number(rule.cameraId) === Number(selectedCamera.id))
    : [];
  const selectedAlerts = selectedCamera
    ? alerts.filter((alert) => Number(alert.cameraId) === Number(selectedCamera.id))
    : alerts;
  const mode = modeFromDetectionType(ruleDraft.detectionType);
  const lineRule = mode === 'line_crossing' || mode === 'multi_line_crossing';
  const crowdRule = mode === 'crowd';
  const lprRule = mode === 'lpr';
  const lineRuleConfig = parseLineRuleConfig(ruleDraft.ruleConfig, mode === 'multi_line_crossing' ? 'multi_line_crossing' : 'line_crossing');
  const crowdRuleConfig = parseCrowdRuleConfig(ruleDraft.ruleConfig);
  const lprRuleConfig = parseLPRRuleConfig(ruleDraft.ruleConfig);
  const targetClasses = targetClassesFromRule(ruleDraft);
  const ruleDestinations = ruleDestinationsFromConfig(ruleDraft.ruleConfig);
  const destinationOptions = (destinations || []).filter((d) => d && d.id);
  const classGroups = groupedClassOptions(classes);
  const selectedZonePoints = parseZonePolygon(ruleDraft.zonePolygon);
  const scheduleDraft = scheduleDraftFromPolicy(ruleDraft.schedulePolicy);
  const [logSelectedAlertId, setLogSelectedAlertId] = useState(null);
  const [aiView, setAiView] = useState('rules');
  // Which rule's inline editor is expanded: a rule id, the sentinel 'new' for an
  // unsaved new rule, or null for none. The editor form is rendered inside the
  // open accordion row and is bound to the single shared ruleDraft.
  const [openRuleId, setOpenRuleId] = useState(null);
  // LPR capability of the selected camera — gates the "License plate" mode option
  // so it only appears on cameras that can supply plate-legible frames. null while
  // unknown/loading (option stays available so we never hide it spuriously).
  const [lprCap, setLprCap] = useState(null);

  useEffect(() => {
    let cancelled = false;
    setLprCap(null);
    if (!selectedCameraId) return undefined;
    (async () => {
      try {
        const headers = authHeader ? { Authorization: authHeader } : {};
        const resp = await fetch(`${apiBase()}/api/cameras/${selectedCameraId}/lpr-capability`, { credentials: 'include', headers });
        const text = await resp.text();
        const payload = text ? JSON.parse(text) : null;
        const result = payload?.data?.result ?? payload?.result ?? payload;
        if (!cancelled) setLprCap(result || null);
      } catch (_) { if (!cancelled) setLprCap(null); }
    })();
    return () => { cancelled = true; };
  }, [selectedCameraId, authHeader]);

  // The LPR mode is offered unless the camera is known to be too low-resolution.
  // Keeping the current rule's mode visible avoids an existing LPR rule vanishing.
  const lprAllowed = !lprCap || lprCap.supported || mode === 'lpr';
  const availableModes = detectionModes.filter(([value]) => value !== 'lpr' || lprAllowed);

  // Auto-close the editor when the shared draft no longer matches the open rule —
  // e.g. after a successful save resets the draft to a blank new rule. Opening a
  // rule sets the draft and openRuleId together (batched), so this never fires
  // mid-open. The 'new' editor stays open (its draft id is always empty) so the
  // user can add another rule immediately.
  useEffect(() => {
    if (openRuleId !== null && openRuleId !== 'new' && Number(ruleDraft.id) !== Number(openRuleId)) {
      setOpenRuleId(null);
    }
  }, [ruleDraft.id, openRuleId]);

  // Alert Log — self-contained server-paged state
  const logPageSize = 20;
  const [logPage, setLogPage] = useState(0);
  const [logDate, setLogDate] = useState(todayDateString);
  const [logAlerts, setLogAlerts] = useState([]);
  const [logTotal, setLogTotal] = useState(0);
  const [logLoading, setLogLoading] = useState(false);
  // Default to real detections only — the Vision monitor's "sampled" diagnostics
  // are hidden unless the user explicitly selects the Diagnostic filter.
  const [logStatus, setLogStatus] = useState('detections');
  const [logRuleId, setLogRuleId] = useState('');

  const fetchLogAlerts = useCallback(async (cameraId, page, dateStr, status, ruleId) => {
    if (!cameraId) return;
    setLogLoading(true);
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const params = new URLSearchParams({
        limit: String(logPageSize),
        offset: String(page * logPageSize),
        cameraId: String(cameraId),
      });
      if (status) {
        params.set('status', status);
      }
      if (ruleId) {
        params.set('ruleId', String(ruleId));
      }
      if (dateStr) {
        const start = new Date(dateStr);
        start.setHours(0, 0, 0, 0);
        const end = new Date(dateStr);
        end.setHours(23, 59, 59, 999);
        params.set('createdAfter', String(Math.floor(start.getTime() / 1000)));
        params.set('createdBefore', String(Math.floor(end.getTime() / 1000)));
      }
      const resp = await fetch(`${apiBase()}/api/vision/alerts?${params}`, { credentials: 'include', headers });
      if (!resp.ok) throw new Error(`${resp.status}`);
      const payload = await resp.json();
      const result = payload?.data?.result ?? payload?.result ?? payload;
      setLogAlerts(Array.isArray(result?.items) ? result.items : []);
      setLogTotal(typeof result?.total === 'number' ? result.total : 0);
    } catch (_) {
      setLogAlerts([]);
      setLogTotal(0);
    } finally {
      setLogLoading(false);
    }
  }, [authHeader]);

  // purgeLogAlerts clears alert rows server-side (days=0 → everything up to now),
  // then reloads. onlyDiagnostics=true removes just the Vision-monitor diagnostics;
  // false also clears real detections (and their snapshot files). Global, not
  // per-camera — diagnostics are noise across all cameras.
  const purgeLogAlerts = useCallback(async (onlyDiagnostics) => {
    setLogLoading(true);
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const params = new URLSearchParams({ days: '0' });
      if (onlyDiagnostics) params.set('onlyDiagnostics', 'true');
      const resp = await fetch(`${apiBase()}/api/vision/alerts/purge?${params}`, { method: 'POST', credentials: 'include', headers });
      if (!resp.ok) throw new Error(`${resp.status}`);
    } catch (_) {
      // Swallow — the reload below reflects whatever state the server is in.
    } finally {
      setLogLoading(false);
    }
    setLogPage(0);
    fetchLogAlerts(selectedCamera?.id, 0, logDate, logStatus, logRuleId);
  }, [authHeader, fetchLogAlerts, selectedCamera?.id, logDate, logStatus, logRuleId]);

  useEffect(() => {
    setLogPage(0);
  }, [selectedCamera?.id, logDate, logStatus, logRuleId]);

  useEffect(() => {
    fetchLogAlerts(selectedCamera?.id, logPage, logDate, logStatus, logRuleId);
  }, [selectedCamera?.id, logPage, logDate, logStatus, logRuleId, fetchLogAlerts]);

  // Rules are camera-specific, so clear the rule filter when the camera changes.
  useEffect(() => {
    setLogRuleId('');
  }, [selectedCamera?.id]);

  useEffect(() => {
    if (!selectedCamera) {
      return;
    }
    if (Number(ruleDraft.cameraId) !== Number(selectedCamera.id)) {
      onRuleDraft({ ...defaultVisionRuleDraft(selectedCamera.id), id: 0 });
    }
  }, [selectedCamera?.id, ruleDraft.cameraId]);

  useEffect(() => {
    if (selectedCamera && onPrepareCamera) {
      onPrepareCamera(selectedCamera).catch(() => {});
    }
  }, [selectedCamera?.id]);

  useEffect(() => {
    if (logSelectedAlertId !== null && !logAlerts.some((alert) => Number(alert.id) === Number(logSelectedAlertId))) {
      setLogSelectedAlertId(null);
    }
  }, [logAlerts, logSelectedAlertId]);

  function selectCamera(cameraId) {
    setOpenRuleId(null);
    onRuleDraft(defaultVisionRuleDraft(cameraId));
  }

  // toggleRule expands a rule into the inline editor (loading it into the shared
  // draft) or collapses it when it is already open.
  function toggleRule(rule) {
    if (openRuleId === rule.id) {
      setOpenRuleId(null);
      return;
    }
    onEditRule(rule);
    setOpenRuleId(rule.id);
  }

  // addRule opens a blank new-rule editor for the selected camera.
  function addRule() {
    onRuleDraft(defaultVisionRuleDraft(selectedCamera.id));
    setOpenRuleId('new');
  }

  function changeSchedulePreset(preset) {
    onRuleDraft({ ...ruleDraft, schedulePolicy: schedulePresetPolicy(preset, scheduleDraft) });
  }

  function changeCustomSchedule(patch) {
    const next = { ...scheduleDraft, ...patch, preset: 'custom' };
    onRuleDraft({ ...ruleDraft, schedulePolicy: weeklySchedulePolicy(next) });
  }

  function changeRangeSchedule(patch) {
    const next = { ...scheduleDraft, ...patch, preset: 'range' };
    onRuleDraft({ ...ruleDraft, schedulePolicy: rangeSchedulePolicy(next) });
  }

  // changeMode switches the detection behavior, preserving the chosen target
  // classes and seeding mode-specific config (line geometry, crowd minCount).
  function changeMode(nextMode) {
    const detectionType = detectionTypeForMode(nextMode);
    const ruleConfig = applyRuleDestinations(buildRuleConfigForMode(nextMode, targetClasses, ruleDraft.ruleConfig), ruleDestinations);
    onRuleDraft({ ...ruleDraft, detectionType, ruleConfig, zonePolygon: ruleDraft.zonePolygon || defaultZonePolygon });
  }

  // changeTargets rewrites the rule's target class list within the current mode.
  function changeTargets(nextTargets) {
    onRuleDraft({ ...ruleDraft, ruleConfig: applyRuleDestinations(buildRuleConfigForMode(mode, nextTargets, ruleDraft.ruleConfig), ruleDestinations) });
  }

  // changeDestinations sets the per-rule routing (which destinations this rule's
  // alerts go to). Empty selection = route to all destinations.
  function changeDestinations(nextIds) {
    onRuleDraft({ ...ruleDraft, ruleConfig: applyRuleDestinations(ruleDraft.ruleConfig, nextIds) });
  }
  function toggleDestination(id, checked) {
    const next = new Set(ruleDestinations);
    if (checked) next.add(id); else next.delete(id);
    changeDestinations(Array.from(next));
  }

  function toggleTarget(slug, checked) {
    const current = new Set(targetClasses.filter((c) => c !== '*'));
    if (checked) {
      current.add(slug);
    } else {
      current.delete(slug);
    }
    changeTargets(Array.from(current));
  }

  function changeLineConfig(patch) {
    const type = mode === 'multi_line_crossing' ? 'multi_line_crossing' : 'line_crossing';
    const next = normalizeLineConfig({ ...lineRuleConfig, ...patch }, type);
    onRuleDraft({ ...ruleDraft, ruleConfig: applyRuleDestinations(lineRuleConfigText(next, type), ruleDestinations) });
  }

  function changeCrowdConfig(patch) {
    const minCount = patch.minCount != null ? patch.minCount : crowdRuleConfig.minCount;
    onRuleDraft({ ...ruleDraft, ruleConfig: applyRuleDestinations(buildRuleConfigForMode('crowd', targetClasses, JSON.stringify({ minCount })), ruleDestinations) });
  }

  function changeLprConfig(patch) {
    const next = { ...lprRuleConfig, ...patch };
    onRuleDraft({ ...ruleDraft, ruleConfig: applyRuleDestinations(lprRuleConfigText(next), ruleDestinations) });
  }
  // The watchlist is edited as free text (one plate per line / comma-separated);
  // parse + normalize on change so stored plates compare cleanly with OCR reads.
  function changeLprPlates(text) {
    const plates = String(text || '')
      .split(/[\n,]+/)
      .map((p) => p.trim().toUpperCase().replace(/[^A-Z0-9]/g, ''))
      .filter(Boolean);
    changeLprConfig({ plates });
  }

  function toggleScheduleDay(day) {
    const current = new Set(scheduleDraft.days);
    if (current.has(day)) {
      current.delete(day);
    } else {
      current.add(day);
    }
    const days = scheduleDayOptions.map(([id]) => id).filter((id) => current.has(id));
    if (days.length === 0) {
      return;
    }
    changeCustomSchedule({ days });
  }

  // ruleRows is the accordion's data: the camera's rules, plus a synthetic
  // trailing row (__newRule) when adding so the new-rule editor opens inline like
  // any other row.
  const ruleRows = openRuleId === 'new'
    ? [...selectedRules, { id: 'new', __newRule: true }]
    : selectedRules;

  return (
    <section className="workspace">
      <div className="toolbar">
        <div>
          <h2 className="section-title">{t('vi.aiDetection')}</h2>
          <p className="section-subtitle">{aiView === 'classes' ? t('vi.subClasses') : t('vi.subRules')}</p>
        </div>
        <button type="button" className="quiet" onClick={onReload} disabled={busy}>
          <span className="btn-icon"><Ico n="reload" /> {t('common.reload')}</span>
        </button>
      </div>

      <nav className="vision-subnav" aria-label={t('vi.aiSectionAria')}>
        <button type="button" className={aiView === 'rules' ? 'active' : 'quiet'} onClick={() => setAiView('rules')}>
          <span className="btn-icon"><Ico n="list" /> {t('vi.detectionRules')}</span>
        </button>
        <button type="button" className={aiView === 'classes' ? 'active' : 'quiet'} onClick={() => setAiView('classes')}>
          <span className="btn-icon"><Ico n="grid2" /> {t('vi.objectClasses')}</span>
        </button>
      </nav>

      {aiView === 'classes' ? (
        <ObjectClassesPanel classes={classes} labelCatalog={labelCatalog} activeModelClasses={activeModelClasses} busy={busy} onSaveClass={onSaveClass} onDeleteClass={onDeleteClass} />
      ) : null}

      {aiView === 'rules' ? (
      <section className="saved-browser vision-browser">
        <SavedDeviceNav devices={saved} selectedId={selectedCamera?.id} onSelect={selectCamera} />
        <main className="saved-detail">
          {selectedCamera ? (
            <>
              <section className="settings-panel">
                <header>
                  <div>
                    <h2>{t('vi.rulesFor', { name: cameraTitle(selectedCamera) })}</h2>
                    <p className="section-subtitle">{selectedCamera.host || selectedCamera.xAddr || t('vi.savedCamera')}</p>
                  </div>
                  <span className="status-pill">{t('vi.rulesCount', { n: selectedRules.length })}</span>
                </header>
                {selectedRules.length === 0 && openRuleId !== 'new' ? (
                  <p className="empty">{t('vi.noRules')}</p>
                ) : null}
                <AccordionList>
                  {ruleRows.map((rule) => {
                    const isNew = rule.__newRule === true;
                    const open = isNew ? true : openRuleId === rule.id;
                    return (
                      <AccordionItem
                        key={rule.id}
                        open={open}
                        onToggle={() => (isNew ? setOpenRuleId(null) : toggleRule(rule))}
                        summary={isNew ? (
                          <span className="accordion-title">{t('vi.newRule')}</span>
                        ) : (
                          <>
                            <span className="accordion-title">{rule.name || rule.detectionType}</span>
                            <span className="accordion-muted">
                              {rule.detectionType} · {t('vi.thresholdLabel', { v: Number(rule.threshold || 0).toFixed(2) })}
                              {lineCountFromRule(rule) ? ` · ${lineCountFromRule(rule)}` : ''} · {scheduleSummary(rule.schedulePolicy)}
                            </span>
                            <span className={`status-pill ${rule.isEnabled ? 'online' : 'unknown'}`}>{rule.isEnabled ? t('vi.enabled') : t('vi.disabled')}</span>
                          </>
                        )}
                        actions={isNew ? null : (
                          <>
                            <button type="button" className="quiet" onClick={() => onTriggerTestAlert(rule)} disabled={busy}>
                              <span className="btn-icon"><Ico n="play" /> {t('common.test')}</span>
                            </button>
                            <button type="button" className="quiet danger-text" onClick={() => onDeleteRule(rule.id)} disabled={busy}>
                              <span className="btn-icon"><Ico n="trash" /> {t('common.delete')}</span>
                            </button>
                          </>
                        )}
                      >
                        {open ? (
                          <form className="vision-rule-form" onSubmit={onSaveRule}>
                            <FormBusyOverlay busy={busy} />
                  <div className="metadata-row">
                    <label>
                      {t('vi.ruleName')}
                      <input
                        value={ruleDraft.name || ''}
                        onChange={(event) => onRuleDraft({ ...ruleDraft, name: event.target.value })}
                        placeholder={t('vi.fireWatchPh', { name: cameraTitle(selectedCamera) })}
                      />
                    </label>
                    <label>
                      {t('vi.modeHow')}
                      <select value={mode} onChange={(event) => changeMode(event.target.value)}>
                        {availableModes.map(([value]) => (
                          <option key={value} value={value}>{t(`vi.mode_${value}`)}</option>
                        ))}
                      </select>
                      {lprCap && !lprCap.supported ? (
                        <span className="field-hint">{t('vi.lprHidden', { detail: lprCap.detail || t('vi.lprHiddenDefault') })}</span>
                      ) : (lprRule && lprCap && lprCap.onvif ? (
                        <span className="field-hint">{t('vi.lprAutoStream', { detail: lprCap.detail })}</span>
                      ) : null)}
                    </label>
                  </div>
                  {crowdRule ? (
                    <section className="schedule-panel">
                      <header>
                        <h3>{t('vi.crowdThreshold')}</h3>
                        <span className="status-pill">{t('vi.peoplePlus', { n: crowdRuleConfig.minCount })}</span>
                      </header>
                      <span className="field-hint">
                        {t('vi.crowdHint')}
                      </span>
                      <div className="metadata-row">
                        <label>
                          {t('vi.minPeople')}
                          <input
                            type="number"
                            min="2"
                            max="100"
                            step="1"
                            value={crowdRuleConfig.minCount}
                            onChange={(event) => changeCrowdConfig({ minCount: Number(event.target.value) })}
                          />
                        </label>
                      </div>
                    </section>
                  ) : null}
                  {lprRule ? (
                    <section className="schedule-panel">
                      <header>
                        <h3>{t('vi.lprTitle')}</h3>
                        <span className="status-pill">
                          {lprRuleConfig.matchMode === 'any' ? t('vi.anyPlate')
                            : lprRuleConfig.matchMode === 'include' ? t('vi.watchN', { n: lprRuleConfig.plates.length })
                            : t('vi.unknownKnown', { n: lprRuleConfig.plates.length })}
                        </span>
                      </header>
                      <span className="field-hint">
                        {t('vi.lprHint')}
                      </span>
                      <div className="metadata-row">
                        <label>
                          {t('vi.whenAlert')}
                          <select value={lprRuleConfig.matchMode} onChange={(event) => changeLprConfig({ matchMode: event.target.value })}>
                            <option value="any">{t('vi.lprAny')}</option>
                            <option value="include">{t('vi.lprInclude')}</option>
                            <option value="exclude">{t('vi.lprExclude')}</option>
                          </select>
                        </label>
                        <label>
                          {t('vi.minOcr')}
                          <input
                            type="number"
                            min="0.1"
                            max="1"
                            step="0.05"
                            value={lprRuleConfig.minOcrConfidence}
                            onChange={(event) => changeLprConfig({ minOcrConfidence: Number(event.target.value) })}
                          />
                        </label>
                      </div>
                      {lprRuleConfig.matchMode !== 'any' ? (
                        <label>
                          {t('vi.plateList', { sub: lprRuleConfig.matchMode === 'include' ? t('vi.plateAlertOnThese') : t('vi.plateKnownAllowed') })}
                          <textarea
                            rows="4"
                            value={lprRuleConfig.plates.join('\n')}
                            onChange={(event) => changeLprPlates(event.target.value)}
                            placeholder={'WXY1234\nABC123'}
                          />
                          <span className="field-hint">{t('vi.platesHint')}</span>
                        </label>
                      ) : null}
                    </section>
                  ) : null}
                  {lineRule ? (
                    <section className="schedule-panel">
                      <header>
                        <h3>{t('vi.lineDirection')}</h3>
                        <span className="status-pill">{lineRuleConfig.direction}</span>
                      </header>
                      <span className="field-hint">{t('vi.lineDirHint')}</span>
                      <div className="metadata-row">
                        <label>
                          {t('vi.direction')}
                          <select value={lineRuleConfig.direction} onChange={(event) => changeLineConfig({ direction: event.target.value })}>
                            <option value="both">{t('vi.dirBoth')}</option>
                            <option value="forward">{t('vi.dirForward')}</option>
                            <option value="reverse">{t('vi.dirReverse')}</option>
                          </select>
                        </label>
                        {mode === 'multi_line_crossing' ? (
                          <label>
                            {t('vi.maxSecBetween')}
                            <input
                              type="number"
                              min="1"
                              value={lineRuleConfig.maxSecondsBetweenLines}
                              onChange={(event) => changeLineConfig({ maxSecondsBetweenLines: Number(event.target.value) })}
                            />
                          </label>
                        ) : null}
                      </div>
                    </section>
                  ) : null}
                  <section className="schedule-panel" style={lprRule ? { display: 'none' } : undefined}>
                    <header>
                      <h3>{t('vi.detectWhat')}</h3>
                      <span className="status-pill">
                        {targetClasses.includes('*') ? t('vi.anything') : t('vi.nSelected', { n: targetClasses.length })}
                      </span>
                    </header>
                    <span className="field-hint">{t('vi.detectHint')}</span>
                    <div className="schedule-days">
                      <label className="check-row line-class-any">
                        <input
                          type="checkbox"
                          checked={targetClasses.includes('*')}
                          onChange={(event) => changeTargets(event.target.checked ? ['*'] : ['person'])}
                        />
                        {t('vi.anythingRow')}
                      </label>
                      {!targetClasses.includes('*') && classGroups.map(([groupLabel, items]) => (
                        <div key={groupLabel} className="class-target-group">
                          <strong className="class-target-group-label">{groupLabel}</strong>
                          {items.map((cls) => {
                            const live = classIsLive(cls, activeModelClasses);
                            return (
                              <label className={`check-row${live ? '' : ' class-inactive'}`} key={cls.name}>
                                <input
                                  type="checkbox"
                                  checked={targetClasses.includes(cls.name)}
                                  onChange={(event) => toggleTarget(cls.name, event.target.checked)}
                                />
                                {cls.displayName || classDisplayName(classes, cls.name)}
                                {!live ? <span className="class-inactive-tag">{t('vi.modelNotActiveInline')}</span> : null}
                              </label>
                            );
                          })}
                        </div>
                      ))}
                      {classGroups.length === 0 ? (
                        <span className="field-hint">{t('vi.noClassesYet')}</span>
                      ) : null}
                    </div>
                  </section>
                  <div className="metadata-row">
                    <label>
                      {t('vi.threshold')}
                      <input
                        type="number"
                        min="0.01"
                        max="1"
                        step="0.01"
                        value={ruleDraft.threshold}
                        onChange={(event) => onRuleDraft({ ...ruleDraft, threshold: Number(event.target.value) })}
                      />
                    </label>
                    <label>
                      {t('vi.minFrames')}
                      <input
                        type="number"
                        min="1"
                        value={ruleDraft.minFrames}
                        onChange={(event) => onRuleDraft({ ...ruleDraft, minFrames: Number(event.target.value) })}
                      />
                    </label>
                  </div>
                  <label>
                    {t('vi.cooldownSeconds')}
                    <input
                      type="number"
                      min="0"
                      value={ruleDraft.cooldownSeconds}
                      onChange={(event) => onRuleDraft({ ...ruleDraft, cooldownSeconds: Number(event.target.value) })}
                    />
                  </label>
                  <section className="schedule-panel">
                    <header>
                      <h3>{t('vi.notifRouting')}</h3>
                      <span className="status-pill">{ruleDestinations.length === 0 ? t('vi.allDestinations') : t('vi.nSelected', { n: ruleDestinations.length })}</span>
                    </header>
                    {destinationOptions.length === 0 ? (
                      <p className="field-hint">{t('vi.noDestinations')}</p>
                    ) : (
                      <>
                        <p className="field-hint">{t('vi.chooseDestinations')}</p>
                        <div className="schedule-days" aria-label={t('vi.notifDestAria')}>
                          {destinationOptions.map((d) => (
                            <label className="check-row" key={d.id}>
                              <input
                                type="checkbox"
                                checked={ruleDestinations.includes(String(d.id))}
                                onChange={(event) => toggleDestination(String(d.id), event.target.checked)}
                              />
                              {d.name || d.type}{d.enabled === false ? t('vi.destDisabledSuffix') : ''}
                            </label>
                          ))}
                        </div>
                      </>
                    )}
                  </section>
                  <section className="schedule-panel">
                    <header>
                      <h3>{t('vi.schedule')}</h3>
                      <span className="status-pill">{scheduleSummary(ruleDraft.schedulePolicy)}</span>
                    </header>
                    <div className="metadata-row">
                      <label>
                        {t('vi.detectionSchedule')}
                        <select value={scheduleDraft.preset} onChange={(event) => changeSchedulePreset(event.target.value)}>
                          <option value="always">{t('vi.schedAlways')}</option>
                          <option value="daytime">{t('vi.schedDaytime')}</option>
                          <option value="nighttime">{t('vi.schedNighttime')}</option>
                          <option value="weekdays">{t('vi.schedWeekdays')}</option>
                          <option value="weekends">{t('vi.schedWeekends')}</option>
                          <option value="custom">{t('vi.schedCustom')}</option>
                          <option value="range">{t('vi.schedRange')}</option>
                        </select>
                      </label>
                      {scheduleDraft.preset === 'custom' || scheduleDraft.preset === 'range' ? (
                        <label>
                          {t('vi.policyMode')}
                          <select
                            value={scheduleDraft.mode}
                            onChange={(event) => {
                              if (scheduleDraft.preset === 'range') {
                                changeRangeSchedule({ mode: event.target.value });
                              } else {
                                changeCustomSchedule({ mode: event.target.value });
                              }
                            }}
                          >
                            <option value="allow">{t('vi.policyAllow')}</option>
                            <option value="deny">{t('vi.policyDeny')}</option>
                          </select>
                        </label>
                      ) : null}
                    </div>
                    {scheduleDraft.preset === 'custom' ? (
                      <>
                        <label>
                          {t('vi.timezone')}
                          <input
                            value={scheduleDraft.timezone}
                            onChange={(event) => changeCustomSchedule({ timezone: event.target.value })}
                            placeholder="Asia/Kuala_Lumpur"
                            autoComplete="off"
                          />
                        </label>
                        <div className="schedule-edit-block">
                          <strong>{t('vi.activeDays')}</strong>
                          <div className="schedule-days" aria-label={t('vi.scheduleDaysAria')}>
                            {scheduleDayOptions.map(([day]) => (
                              <label className="check-row" key={day}>
                                <input
                                  type="checkbox"
                                  checked={scheduleDraft.days.includes(day)}
                                  onChange={() => toggleScheduleDay(day)}
                                />
                                {t(`vi.day_${day}`)}
                              </label>
                            ))}
                          </div>
                        </div>
                        <div className="metadata-row">
                          <label>
                            {t('vi.startTime')}
                            <input
                              type="time"
                              value={scheduleDraft.start}
                              onChange={(event) => changeCustomSchedule({ start: event.target.value })}
                            />
                          </label>
                          <label>
                            {t('vi.endTime')}
                            <input
                              type="time"
                              value={scheduleDraft.end}
                              onChange={(event) => changeCustomSchedule({ end: event.target.value })}
                            />
                          </label>
                        </div>
                      </>
                    ) : null}
                    {scheduleDraft.preset === 'range' ? (
                      <>
                        <label>
                          {t('vi.timezone')}
                          <input
                            value={scheduleDraft.timezone}
                            onChange={(event) => changeRangeSchedule({ timezone: event.target.value })}
                            placeholder="Asia/Kuala_Lumpur"
                            autoComplete="off"
                          />
                        </label>
                        <div className="metadata-row">
                          <label>
                            {t('vi.startDatetime')}
                            <input
                              type="datetime-local"
                              value={scheduleDraft.rangeStart}
                              onChange={(event) => changeRangeSchedule({ rangeStart: event.target.value })}
                            />
                          </label>
                          <label>
                            {t('vi.endDatetime')}
                            <input
                              type="datetime-local"
                              value={scheduleDraft.rangeEnd}
                              onChange={(event) => changeRangeSchedule({ rangeEnd: event.target.value })}
                            />
                          </label>
                        </div>
                      </>
                    ) : null}
                  </section>
                  {lineRule ? (
                    <LineDrawingPreview
                      camera={selectedCamera}
                      config={lineRuleConfig}
                      detectionType={ruleDraft.detectionType}
                      authHeader={authHeader}
                      streamConfig={streamConfig}
                      disabled={busy}
                      onConfig={changeLineConfig}
                    />
                  ) : (
                    <ZoneDrawingPreview
                      camera={selectedCamera}
                      polygonValue={ruleDraft.zonePolygon}
                      authHeader={authHeader}
                      streamConfig={streamConfig}
                      disabled={busy}
                      onPolygon={(zonePolygon) => onRuleDraft({ ...ruleDraft, cameraId: selectedCamera.id, zonePolygon })}
                    />
                  )}
                  <div className="action-row">
                    <label className="check-row">
                      <input
                        type="checkbox"
                        checked={Boolean(ruleDraft.soundEnabled)}
                        onChange={(event) => onRuleDraft({ ...ruleDraft, soundEnabled: event.target.checked })}
                      />
                      {t('vi.soundAlert')}
                    </label>
                    <label className="check-row">
                      <input
                        type="checkbox"
                        checked={Boolean(ruleDraft.isEnabled)}
                        onChange={(event) => onRuleDraft({ ...ruleDraft, isEnabled: event.target.checked })}
                      />
                      {t('common.enabled')}
                    </label>
                  </div>
                  <div className="action-row">
                    <button type="submit" disabled={busy || (!lprRule && targetClasses.length < 1) || (!lineRule && selectedZonePoints.length < 3) || (lineRule && lineRuleConfig.lines.length < (mode === 'multi_line_crossing' ? 2 : 1)) || (lprRule && lprRuleConfig.matchMode !== 'any' && lprRuleConfig.plates.length < 1)}>
                      <span className="btn-icon"><Ico n="save" /> {t('vi.saveRule')}</span>
                    </button>
                    <button type="button" className="quiet" onClick={() => setOpenRuleId(null)} disabled={busy}>
                      {t('common.cancel')}
                    </button>
                  </div>
                          </form>
                        ) : null}
                      </AccordionItem>
                    );
                  })}
                </AccordionList>
                <div className="action-row">
                  <button type="button" className="quiet" onClick={addRule} disabled={busy || openRuleId === 'new'}>
                    <span className="btn-icon"><Ico n="plus" /> {t('vi.addRule')}</span>
                  </button>
                </div>
              </section>

              <section className="settings-panel">
                <header>
                  <h2>{t('vi.alertLog')}</h2>
                  <span className="status-pill">{logLoading ? '…' : logTotal}</span>
                </header>
                <div className="vision-list">
                  <div className="log-toolbar" style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginBottom: '0.5rem', flexWrap: 'wrap' }}>
                    <label style={{ display: 'flex', gap: '0.4rem', alignItems: 'center', margin: 0 }}>
                      {t('vi.date')}
                      <input
                        type="date"
                        value={logDate}
                        max={todayDateString()}
                        onChange={(e) => setLogDate(e.target.value)}
                      />
                    </label>
                    <label style={{ display: 'flex', gap: '0.4rem', alignItems: 'center', margin: 0 }}>
                      {t('common.status')}
                      <select value={logStatus} onChange={(e) => setLogStatus(e.target.value)}>
                        <option value="detections">{t('vi.statusAllDetections')}</option>
                        <option value="active">{t('vi.statusActive')}</option>
                        <option value="acknowledged">{t('vi.statusAcknowledged')}</option>
                        <option value="diagnostic">{t('vi.statusDiagnostic')}</option>
                        <option value="">{t('vi.statusAllInclDiag')}</option>
                      </select>
                    </label>
                    <label style={{ display: 'flex', gap: '0.4rem', alignItems: 'center', margin: 0 }}>
                      {t('vi.rule')}
                      <select value={logRuleId} onChange={(e) => setLogRuleId(e.target.value)}>
                        <option value="">{t('vi.allRules')}</option>
                        {selectedRules.map((rule) => (
                          <option key={rule.id} value={rule.id}>{rule.name || rule.detectionType || t('vi.ruleNum', { id: rule.id })}</option>
                        ))}
                      </select>
                    </label>
                    <button type="button" className="quiet" onClick={() => setLogDate('')} disabled={!logDate}>
                      {t('vi.allDates')}
                    </button>
                    <button type="button" className="quiet" onClick={() => setLogDate(todayDateString())} disabled={logDate === todayDateString()}>
                      {t('vi.today')}
                    </button>
                    <button type="button" className="quiet" onClick={() => fetchLogAlerts(selectedCamera?.id, logPage, logDate, logStatus, logRuleId)} disabled={logLoading}>
                      {t('common.reload')}
                    </button>
                    <button
                      type="button"
                      className="quiet danger-text"
                      onClick={() => {
                        if (window.confirm(t('vi.clearDiagConfirm'))) {
                          purgeLogAlerts(true);
                        }
                      }}
                      disabled={logLoading}
                      title={t('vi.clearDiagTitle')}
                    >
                      <span className="btn-icon"><Ico n="trash" /> {t('vi.clearDiagnostics')}</span>
                    </button>
                  </div>
                  {logLoading ? <p className="empty">{t('vi.loading')}</p> : null}
                  {!logLoading && logAlerts.length === 0 ? <p className="empty">{t('vi.noEvents', { date: logDate ? t('vi.onThisDate') : '' })}</p> : null}
                  {logAlerts.length > 0 ? (
                    <div className="event-table-wrap">
                      <table className="event-table">
                        <thead>
                          <tr>
                            <th>{t('vi.thTime')}</th>
                            <th>{t('vi.thEvent')}</th>
                            <th>{t('vi.rule')}</th>
                            <th>{t('vi.thConfidence')}</th>
                            <th>{t('common.status')}</th>
                            <th>{t('vi.thAction')}</th>
                          </tr>
                        </thead>
                        <tbody>
                          {logAlerts.map((alert) => {
                            const metadata = parseMetadata(alert.metadata);
                            const diagnostic = Boolean(metadata.diagnostic);
                            const rule = selectedRules.find((item) => Number(item.id) === Number(alert.ruleId));
                            const objectLabel = metadata.objectLabel;
                            return (
                              <tr key={alert.id} className={Number(logSelectedAlertId) === Number(alert.id) ? 'selected' : ''}>
                                <td>{formatTimestamp(alert.createdAt)}</td>
                                <td>
                                  <strong>{objectLabel || alert.label || alert.detectionType || t('vi.detectionEvent')}</strong>
                                  <span>{formatSourceLabel(metadata.source)}</span>
                                </td>
                                <td>{rule?.name || `#${alert.ruleId || '-'}`}</td>
                                <td>{Number(alert.confidence || 0).toFixed(3)}</td>
                                <td>
                                  <span className={`status-pill ${diagnostic ? 'unknown' : alert.isAcknowledged ? 'resolved' : 'offline'}`}>
                                    {diagnostic ? t('vi.pillDiagnostic') : alert.isAcknowledged ? t('vi.pillAcknowledged') : t('vi.pillActive')}
                                  </span>
                                </td>
                                <td>
                                  <div className="table-actions">
                                    <button type="button" className="quiet" onClick={() => setLogSelectedAlertId(Number(logSelectedAlertId) === Number(alert.id) ? null : alert.id)}>
                                      {Number(logSelectedAlertId) === Number(alert.id) ? t('common.close') : t('vi.details')}
                                    </button>
                                    <button
                                      type="button"
                                      className="quiet"
                                      onClick={() => onAcknowledgeAlert(alert.id)}
                                      disabled={busy || alert.isAcknowledged || diagnostic}
                                    >
                                      <Ico n="acknowledge" sz={12} />
                                    </button>
                                  </div>
                                </td>
                              </tr>
                            );
                          })}
                        </tbody>
                      </table>
                    </div>
                  ) : null}
                  {logTotal > logPageSize ? (
                    <div className="pagination-bar" style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginTop: '0.5rem' }}>
                      <button type="button" className="quiet" onClick={() => setLogPage((p) => Math.max(0, p - 1))} disabled={logPage === 0 || logLoading}>
                        {t('vi.prevArrow')}
                      </button>
                      <span style={{ fontSize: '0.85rem' }}>
                        {t('vi.pageOf', { n: logPage + 1, total: Math.ceil(logTotal / logPageSize) })}
                      </span>
                      <button type="button" className="quiet" onClick={() => setLogPage((p) => p + 1)} disabled={(logPage + 1) * logPageSize >= logTotal || logLoading}>
                        {t('vi.nextArrow')}
                      </button>
                    </div>
                  ) : null}
                  {null /* detail shown in AlertDetailModal overlay */}
                </div>
              </section>
            </>
          ) : (
            <section className="device-card empty-detail">
              <h2>{t('vi.noCameraSelected')}</h2>
              <p className="empty">{t('vi.noCameraHint')}</p>
            </section>
          )}
        </main>
      </section>
      ) : null}
      {(() => {
        const logSelectedAlert = logAlerts.find((a) => Number(a.id) === Number(logSelectedAlertId)) || null;
        if (!logSelectedAlert) return null;
        const logSelectedAlertRule = selectedRules.find((r) => Number(r.id) === Number(logSelectedAlert.ruleId));
        return (
          <AlertDetailModal
            alert={logSelectedAlert}
            rule={logSelectedAlertRule}
            authHeader={authHeader}
            onClose={() => setLogSelectedAlertId(null)}
          />
        );
      })()}
    </section>
  );
}

// ObjectClassesPanel manages the detection class registry. A "category" (object
// or hazard) maps a friendly name to one or more raw model labels; a "group"
// bundles several categories. Trained labels (e.g. "papa") can be folded into an
// existing category so they aren't a top-level sibling of Person/Vehicle/Animal.
const emptyClassDraft = { id: 0, name: '', displayName: '', kind: 'object', members: [], source: 'object' };

function classSlug(value) {
  return String(value || '').trim().toLowerCase().replace(/\s+/g, '_');
}

// normalizeLabel canonicalizes a RAW model label: lowercased, trimmed, internal
// whitespace collapsed — but spaces are PRESERVED (the detector emits labels like
// "cell phone", so underscoring them would stop them matching). Distinct from
// classSlug, which underscores to make a category id.
function normalizeLabel(value) {
  return String(value || '').trim().toLowerCase().replace(/\s+/g, ' ');
}

function titleizeLabel(value) {
  return normalizeLabel(value).replace(/\b\w/g, (c) => c.toUpperCase());
}

// Picker group that holds the active custom model's labels, pinned to the top so
// user-trained labels (papa, oven, …) are easy to find among the stock catalog.
const TRAINED_GROUP = 'Custom model';

function ObjectClassesPanel({ classes, labelCatalog, activeModelClasses, busy, onSaveClass, onDeleteClass }) {
  const t = useT();
  const [draft, setDraft] = useState(emptyClassDraft);
  const [labelInput, setLabelInput] = useState('');
  // The new/edit form lives in a modal (matching the image-labelling popup) so the
  // class list stays the focus; opening it seeds the draft, closing resets it.
  const [editorOpen, setEditorOpen] = useState(false);
  // Collapsed label groups in the picker (by group name). Empty = all expanded.
  const [collapsedGroups, setCollapsedGroups] = useState(() => new Set());
  const isNew = !draft.id;
  const isGroup = draft.kind === 'group';

  // Categories selectable as group members (everything that isn't a group).
  const selectableMembers = (classes || []).filter((cls) => cls.kind !== 'group' && cls.name !== draft.name);
  // The label catalog (from /api/vision/labels) is the source of pickable labels,
  // each carrying a group + source. Fall back to deriving a flat list from the
  // registry if the catalog endpoint returned nothing, so the picker still works.
  const knownLabels = Array.from(new Set([
    ...(classes || [])
      .filter((cls) => cls.kind !== 'group')
      .flatMap((cls) => (Array.isArray(cls.memberList) ? cls.memberList : [])),
    ...(activeModelClasses || []),
  ].map((label) => normalizeLabel(label)).filter(Boolean))).sort();
  const baseCatalog = Array.isArray(labelCatalog) && labelCatalog.length
    ? labelCatalog
    : knownLabels.map((label) => ({ label, display: label, source: 'stock', group: 'Labels' }));
  // The backend catalog is registry-derived, so the ACTIVE custom model's labels
  // (papa, oven, …) aren't in it until they're mapped to a category. Merge them in
  // from activeModelClasses under a pinned "Custom model" group so they're always
  // offered — and remain detectable, since only an active model produces them.
  const catalog = (() => {
    const byLabel = new Map(baseCatalog.map((entry) => [entry.label, { ...entry }]));
    (activeModelClasses || []).forEach((raw) => {
      const label = normalizeLabel(raw);
      if (!label) return;
      const existing = byLabel.get(label);
      byLabel.set(label, {
        label,
        display: (existing && existing.display) || titleizeLabel(label),
        source: 'trained',
        group: TRAINED_GROUP,
      });
    });
    return Array.from(byLabel.values());
  })();

  // Labels not already in this draft, filtered by the search box (matches label or
  // display), then grouped for browsing. Picking from these avoids typos.
  const labelQuery = labelInput.trim().toLowerCase();
  const availableEntries = catalog.filter((entry) => entry && !draft.members.includes(entry.label));
  const matchingEntries = labelQuery
    ? availableEntries.filter((entry) =>
        entry.label.toLowerCase().includes(labelQuery)
        || String(entry.display || '').toLowerCase().includes(labelQuery))
    : availableEntries;
  const groupedEntries = Array.from(
    matchingEntries.reduce((map, entry) => {
      const group = entry.group || 'Other';
      if (!map.has(group)) map.set(group, []);
      map.get(group).push(entry);
      return map;
    }, new Map()).entries(),
  )
    .map(([group, entries]) => [group, entries.sort((a, b) => String(a.display || a.label).localeCompare(String(b.display || b.label)))])
    .sort((a, b) => {
      // Pin the active custom model's labels to the top, then alphabetical.
      if (a[0] === TRAINED_GROUP) return -1;
      if (b[0] === TRAINED_GROUP) return 1;
      return a[0].localeCompare(b[0]);
    });
  const searching = Boolean(labelQuery);
  // The typed term can be added as a brand-new label only when it isn't already a
  // known label or an existing member (keeps typo-prone free entry behind intent).
  const customLabel = normalizeLabel(labelInput);
  const knownLabelSet = new Set(catalog.map((entry) => entry.label));
  const customAddable = Boolean(customLabel) && !knownLabelSet.has(customLabel) && !draft.members.includes(customLabel);

  function editClass(cls) {
    setLabelInput('');
    setDraft({
      id: cls.id,
      name: cls.name,
      displayName: cls.displayName || cls.name,
      kind: cls.kind || 'object',
      members: Array.isArray(cls.memberList) ? cls.memberList : [],
      source: cls.source || 'object',
    });
    setEditorOpen(true);
  }

  function openNew() { setLabelInput(''); setDraft(emptyClassDraft); setEditorOpen(true); }
  function closeEditor() { setLabelInput(''); setDraft(emptyClassDraft); setEditorOpen(false); }

  // Close the modal on Escape, mirroring the image-labelling dialog.
  useEffect(() => {
    if (!editorOpen) return undefined;
    const onKey = (e) => { if (e.key === 'Escape') closeEditor(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [editorOpen]);

  function toggleMember(slug, checked) {
    const current = new Set(draft.members);
    if (checked) current.add(slug); else current.delete(slug);
    setDraft({ ...draft, members: Array.from(current) });
  }

  function addLabel(label) {
    const value = normalizeLabel(label);
    if (!value) return;
    if (!draft.members.includes(value)) setDraft({ ...draft, members: [...draft.members, value] });
    setLabelInput('');
  }

  function toggleGroup(group) {
    setCollapsedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(group)) next.delete(group); else next.add(group);
      return next;
    });
  }

  function removeLabel(label) {
    setDraft({ ...draft, members: draft.members.filter((m) => m !== label) });
  }

  function submit(event) {
    event.preventDefault();
    const display = draft.displayName.trim();
    if (!display) return;
    const members = isGroup
      ? draft.members
      : (draft.members.length ? draft.members : [classSlug(display)]);
    onSaveClass({
      id: draft.id,
      // For a new class the name (slug) is derived from the display name; existing
      // classes keep their slug.
      name: isNew ? classSlug(display) : draft.name,
      displayName: display,
      kind: draft.kind,
      members,
      source: draft.source,
    });
    closeEditor();
  }

  return (
    <section className="settings-panel">
      <header>
        <h2>{t('vi.objectClasses')}</h2>
        <span className="status-pill">{(classes || []).length}</span>
      </header>
      <span className="field-hint">
        {t('vi.classesIntro')}
      </span>

      <div className="action-row">
        <button type="button" onClick={openNew} disabled={busy}>
          <span className="btn-icon"><Ico n="plus" /> {t('vi.addCategoryGroup')}</span>
        </button>
      </div>

      <ul className="class-registry-list">
        {(classes || []).map((cls) => (
          <li key={cls.id} className="class-registry-row">
            <div className="class-registry-info">
              <strong>{cls.displayName || cls.name}</strong>
              <span className={`class-source-badge ${cls.source}`}>{cls.source}</span>
              {cls.source === 'trained' && !classIsLive(cls, activeModelClasses) ? (
                <span className="class-source-badge inactive">{t('vi.modelNotActiveBadge')}</span>
              ) : null}
              <div className="chip-list">
                {(cls.memberList || []).length
                  ? (cls.memberList || []).map((m) => <span key={m} className="chip chip-static">{m}</span>)
                  : <span className="field-hint">{t('vi.noLabels')}</span>}
              </div>
            </div>
            <div className="class-registry-actions">
              <button type="button" className="quiet" onClick={() => editClass(cls)} disabled={busy}>{t('common.edit')}</button>
              {cls.source !== 'builtin' ? (
                <button type="button" className="quiet danger" onClick={() => onDeleteClass(cls.id)} disabled={busy}>{t('common.delete')}</button>
              ) : null}
            </div>
          </li>
        ))}
      </ul>

      {editorOpen ? (
      <div className="video-overlay" onClick={closeEditor}>
      <div className="video-dialog class-editor-dialog" onClick={(e) => e.stopPropagation()}>
        <div className="video-dialog-header">
          <span className="video-dialog-title">{isNew ? t('vi.newCategoryGroup') : t('vi.editQuoted', { name: draft.displayName || draft.name })}</span>
          <button type="button" className="video-dialog-close" onClick={closeEditor} aria-label={t('common.close')}>✕</button>
        </div>
      <form className="vision-rule-form class-editor-form" onSubmit={submit}>
        <div className="metadata-row">
          <label>
            {t('common.name')}
            <input
              value={draft.displayName}
              onChange={(event) => setDraft({ ...draft, displayName: event.target.value })}
              placeholder={t('vi.namePh')}
              disabled={busy || draft.source === 'builtin'}
            />
            {!isNew ? <span className="field-hint">{t('vi.idLabel', { id: draft.name })}</span> : null}
          </label>
          <label>
            {t('common.type')}
            <select
              value={draft.kind}
              onChange={(event) => setDraft({ ...draft, kind: event.target.value })}
              disabled={busy || draft.source === 'builtin'}
            >
              <option value="object">{t('vi.kindObject')}</option>
              <option value="hazard">{t('vi.kindHazard')}</option>
              <option value="group">{t('vi.kindGroup')}</option>
            </select>
          </label>
        </div>

        {isGroup ? (
          <div className="class-member-editor">
            <strong>{t('vi.categoriesInGroup')}</strong>
            <span className="field-hint">{t('vi.groupHint')}</span>
            <div className="class-option-grid">
              {selectableMembers.map((cls) => (
                <label className="check-row class-option" key={cls.name}>
                  <input
                    type="checkbox"
                    checked={draft.members.includes(cls.name)}
                    onChange={(event) => toggleMember(cls.name, event.target.checked)}
                  />
                  {cls.displayName || cls.name}
                </label>
              ))}
              {selectableMembers.length === 0 ? <span className="field-hint">{t('vi.noCategoriesGroup')}</span> : null}
            </div>
          </div>
        ) : (
          <div className="class-member-editor">
            <strong>{t('vi.modelLabelsCategory')}</strong>
            <span className="field-hint">
              {t('vi.labelsHint')}
            </span>

            {draft.members.length ? (
              <div className="chip-list">
                {draft.members.map((label) => (
                  <span key={label} className="chip">
                    {label}
                    <button type="button" className="chip-remove" onClick={() => removeLabel(label)} aria-label={t('vi.removeLabel', { label })} disabled={busy}>×</button>
                  </span>
                ))}
              </div>
            ) : null}

            <div className="class-label-picker">
              <input
                className="class-label-search"
                value={labelInput}
                onChange={(event) => setLabelInput(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key !== 'Enter') return;
                  event.preventDefault();
                  if (matchingEntries.length === 1) addLabel(matchingEntries[0].label);
                  else if (customAddable) addLabel(labelInput);
                }}
                placeholder={catalog.length ? t('vi.searchLabels', { n: catalog.length }) : t('vi.typeLabel')}
                disabled={busy}
              />
              <div className="class-label-groups">
                {groupedEntries.map(([group, entries]) => {
                  const open = searching || !collapsedGroups.has(group);
                  const shown = entries.slice(0, 50);
                  return (
                    <div className="class-label-group" key={group}>
                      <button type="button" className="class-label-group-head" onClick={() => toggleGroup(group)} disabled={searching}>
                        <span className="class-label-caret">{open ? '▾' : '▸'}</span>
                        <strong>{group}</strong>
                        <span className="status-pill">{entries.length}</span>
                      </button>
                      {open ? (
                        <div className="chip-list">
                          {shown.map((entry) => (
                            <button type="button" key={entry.label} className={`chip chip-suggest-btn${entry.source === 'trained' ? ' is-trained' : ''}`} onClick={() => addLabel(entry.label)} disabled={busy} title={entry.label}>
                              + {entry.display || entry.label}
                              {entry.source === 'trained' ? <span className="chip-tag">{t('vi.trained')}</span> : null}
                            </button>
                          ))}
                          {entries.length > shown.length ? (
                            <span className="field-hint">{t('vi.moreSearch', { n: entries.length - shown.length })}</span>
                          ) : null}
                        </div>
                      ) : null}
                    </div>
                  );
                })}
                {availableEntries.length === 0
                  ? <span className="field-hint">{t('vi.everyLabelIn')}</span> : null}
                {availableEntries.length > 0 && matchingEntries.length === 0 && !customAddable
                  ? <span className="field-hint">{t('vi.noLabelsMatch')}</span> : null}
              </div>
              {customAddable ? (
                <button type="button" className="quiet" onClick={() => addLabel(labelInput)} disabled={busy}>
                  <span className="btn-icon"><Ico n="plus" /> {t('vi.addCustomLabel', { label: labelInput.trim() })}</span>
                </button>
              ) : null}
            </div>
          </div>
        )}

        <div className="action-row">
          <button type="submit" disabled={busy || !draft.displayName.trim()}>
            <span className="btn-icon"><Ico n="save" /> {t('common.save')}</span>
          </button>
          <button type="button" className="quiet" onClick={closeEditor} disabled={busy}>{t('common.cancel')}</button>
        </div>
      </form>
      </div>
      </div>
      ) : null}
    </section>
  );
}

export function AlertDetailModal({ alert, rule, authHeader, onClose }) {
  const t = useT();
  const meta = parseMetadata(alert.metadata);
  const bb = parseBoundingBox(alert.boundingBox);
  const isDiagnostic = Boolean(meta.diagnostic);
  const isYolo = meta.source && (meta.source.includes('object') || meta.source.includes('yolo'));
  const isMotion = meta.source && meta.source.includes('motion');
  const isLineCrossing = alert.detectionType === 'line-crossing' || alert.detectionType === 'multi-line-crossing';
  const conf = Number(alert.confidence || 0);
  const objectMeta = meta.objectMeta && typeof meta.objectMeta === 'object' ? meta.objectMeta : {};
  const trackId = meta.trackId || objectMeta.trackId;
  const { url: snapUrl, loading: snapLoading, error: snapError } = useSnapshotBlob(alert.id, authHeader);

  useEffect(() => {
    function onKey(e) { if (e.key === 'Escape') onClose(); }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const title = meta.objectLabel || alert.label || alert.detectionType || t('vi.detectionEvent');

  // Download the snapshot with the detection box drawn in (server-side), matching
  // the notification image. Uses an auth'd fetch since the endpoint is protected.
  async function downloadAnnotated() {
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const res = await fetch(`${apiBase()}/api/vision/alerts/${alert.id}/snapshot?annotated=1`, { credentials: 'include', headers });
      if (!res.ok) throw new Error(res.status);
      const blob = await res.blob();
      const objectUrl = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = objectUrl;
      link.download = `alert-${alert.id}.jpg`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(objectUrl);
    } catch {
      // Download is best-effort; ignore failures.
    }
  }

  return (
    <div className="alert-modal-overlay" onClick={onClose}>
      <div className="alert-modal-dialog" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label={title}>
        <div className="alert-modal-header">
          <div className="alert-modal-title-group">
            <span className="alert-modal-title" style={{ textTransform: 'capitalize' }}>{title}</span>
            <span className={`status-pill ${isDiagnostic ? 'unknown' : alert.isAcknowledged ? 'resolved' : 'offline'}`}>
              {isDiagnostic ? t('vi.pillDiagnostic') : alert.isAcknowledged ? t('vi.pillAcknowledged') : t('vi.pillActive')}
            </span>
            <span className="alert-modal-time">{formatTimestamp(alert.createdAt)}</span>
          </div>
          <button type="button" className="alert-modal-close" onClick={onClose} aria-label={t('common.close')}>✕</button>
        </div>

        <div className="alert-modal-image-wrap">
          {snapLoading && <div className="alert-modal-snap-msg">{t('vi.loadingSnapshot')}</div>}
          {snapError && !snapLoading && <div className="alert-modal-snap-msg alert-modal-snap-none">{t('vi.noSnapshot')}</div>}
          {snapUrl && (
            <div className="alert-modal-snap-container">
              <img className="alert-modal-snap" src={snapUrl} alt={t('vi.detectionSnapshot')} />
              {bb && (
                <div className="alert-modal-bb" style={{
                  left: `${(bb.x * 100).toFixed(3)}%`,
                  top: `${(bb.y * 100).toFixed(3)}%`,
                  width: `${(bb.w * 100).toFixed(3)}%`,
                  height: `${(bb.h * 100).toFixed(3)}%`,
                }}>
                  <span className="alert-modal-bb-label">{meta.objectLabel || alert.label || ''}</span>
                </div>
              )}
            </div>
          )}
          {snapUrl && bb && (
            <div className="alert-modal-snap-actions">
              <button type="button" className="quiet" onClick={downloadAnnotated}>
                <span className="btn-icon"><Ico n="download" /> {t('vi.downloadWithBox')}</span>
              </button>
            </div>
          )}
        </div>

        <div className="alert-modal-meta">
          <dl className="event-grid">
            <div><dt>{t('vi.rule')}</dt><dd>{rule?.name || `#${alert.ruleId || '-'}`}</dd></div>
            <div><dt>{t('common.type')}</dt><dd>{fieldValue(alert.detectionType)}</dd></div>
            <div><dt>{t('vi.metaSource')}</dt><dd>{formatSourceLabel(meta.source)}</dd></div>
            <div>
              <dt>{t('vi.thConfidence')}</dt>
              <dd>
                <span className="conf-value">{conf.toFixed(3)}</span>
                <div className="conf-bar-wrap">
                  <div className="conf-bar" style={{ width: `${(conf * 100).toFixed(1)}%`, background: conf >= 0.6 ? 'var(--accent)' : conf >= 0.35 ? 'var(--warn-color, #e8a000)' : 'var(--danger-color, #d9534f)' }} />
                </div>
              </dd>
            </div>
            {isYolo && meta.objectLabel ? <div><dt>{t('vi.objectLabelMeta')}</dt><dd style={{ textTransform: 'capitalize' }}>{meta.objectLabel}</dd></div> : null}
            {trackId ? <div><dt>{t('vi.trackId')}</dt><dd>{trackId}</dd></div> : null}
            {isLineCrossing && meta.lineId ? (
              <div><dt>{t('vi.line')}</dt><dd>{meta.lineId}{meta.lineCount > 1 ? ` (${Number(meta.lineIndex) + 1}/${meta.lineCount})` : ''}</dd></div>
            ) : null}
            {isMotion && meta.changedRatio !== undefined ? <div><dt>{t('vi.changedArea')}</dt><dd>{formatPercent(meta.changedRatio)}</dd></div> : null}
            {isDiagnostic ? (
              <>
                <div><dt>{t('common.status')}</dt><dd>{fieldValue(meta.status)}</dd></div>
                <div><dt>{t('vi.threshold')}</dt><dd>{meta.ruleThreshold !== undefined ? Number(meta.ruleThreshold).toFixed(2) : '-'}</dd></div>
                <div><dt>{t('vi.metaMinFrames')}</dt><dd>{meta.ruleMinFrames ?? '-'}</dd></div>
                <div><dt>{t('vi.cooldown')}</dt><dd>{meta.ruleCooldownSec !== undefined ? `${meta.ruleCooldownSec}s` : '-'}</dd></div>
                {meta.message ? <div style={{ gridColumn: '1 / -1' }}><dt>{t('vi.message')}</dt><dd>{meta.message}</dd></div> : null}
              </>
            ) : null}
            {bb ? <div><dt>{t('vi.boundingBox')}</dt><dd className="bb-coords-inline">X {formatPercent(bb.x)} Y {formatPercent(bb.y)} W {formatPercent(bb.w)} H {formatPercent(bb.h)}</dd></div> : null}
            <div><dt>{t('vi.acknowledgedMeta')}</dt><dd>{alert.isAcknowledged ? formatTimestamp(alert.acknowledgedAt) : '-'}</dd></div>
          </dl>
        </div>
      </div>
    </div>
  );
}

