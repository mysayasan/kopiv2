import { useState, useEffect, useMemo, useCallback } from 'react';
import { Ico } from './icons';
import { FormBusyOverlay } from './ui';
import { useSnapshotBlob } from '../hooks';
import { scheduleDayOptions } from '../lib/constants';
import {todayDateString,apiBase,fieldValue,formatTimestamp,parseMetadata,formatPercent,parseBoundingBox,formatSourceLabel,cameraTitle,orderedSavedCameras,parseZonePolygon,defaultZonePolygon,normalizeLineConfig,parseLineRuleConfig,lineRuleConfigText,lineCountFromRule,parseCrowdRuleConfig,detectionModes,modeFromDetectionType,detectionTypeForMode,targetClassesFromRule,buildRuleConfigForMode,groupedClassOptions,classDisplayName,defaultVisionRuleDraft,weeklySchedulePolicy,rangeSchedulePolicy,schedulePresetPolicy,scheduleDraftFromPolicy,scheduleSummary } from '../lib/helpers';
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
  const lineRuleConfig = parseLineRuleConfig(ruleDraft.ruleConfig, mode === 'multi_line_crossing' ? 'multi_line_crossing' : 'line_crossing');
  const crowdRuleConfig = parseCrowdRuleConfig(ruleDraft.ruleConfig);
  const targetClasses = targetClassesFromRule(ruleDraft);
  const classGroups = groupedClassOptions(classes);
  const selectedZonePoints = parseZonePolygon(ruleDraft.zonePolygon);
  const scheduleDraft = scheduleDraftFromPolicy(ruleDraft.schedulePolicy);
  const [logSelectedAlertId, setLogSelectedAlertId] = useState(null);
  const [aiView, setAiView] = useState('rules');

  // Alert Log — self-contained server-paged state
  const logPageSize = 20;
  const [logPage, setLogPage] = useState(0);
  const [logDate, setLogDate] = useState(todayDateString);
  const [logAlerts, setLogAlerts] = useState([]);
  const [logTotal, setLogTotal] = useState(0);
  const [logLoading, setLogLoading] = useState(false);
  const [logStatus, setLogStatus] = useState('');
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
    onRuleDraft(defaultVisionRuleDraft(cameraId));
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
    const ruleConfig = buildRuleConfigForMode(nextMode, targetClasses, ruleDraft.ruleConfig);
    onRuleDraft({ ...ruleDraft, detectionType, ruleConfig, zonePolygon: ruleDraft.zonePolygon || defaultZonePolygon });
  }

  // changeTargets rewrites the rule's target class list within the current mode.
  function changeTargets(nextTargets) {
    onRuleDraft({ ...ruleDraft, ruleConfig: buildRuleConfigForMode(mode, nextTargets, ruleDraft.ruleConfig) });
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
    onRuleDraft({ ...ruleDraft, ruleConfig: lineRuleConfigText(next, type) });
  }

  function changeCrowdConfig(patch) {
    const minCount = patch.minCount != null ? patch.minCount : crowdRuleConfig.minCount;
    onRuleDraft({ ...ruleDraft, ruleConfig: buildRuleConfigForMode('crowd', targetClasses, JSON.stringify({ minCount })) });
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

  return (
    <section className="workspace">
      <div className="toolbar">
        <div>
          <h2 className="section-title">AI Detection</h2>
          <p className="section-subtitle">{aiView === 'classes' ? 'Object classes used by detection rules.' : 'Camera rules and alert events.'}</p>
        </div>
        <button type="button" className="quiet" onClick={onReload} disabled={busy}>
          <span className="btn-icon"><Ico n="reload" /> Reload</span>
        </button>
      </div>

      <nav className="vision-subnav" aria-label="AI section">
        <button type="button" className={aiView === 'rules' ? 'active' : 'quiet'} onClick={() => setAiView('rules')}>
          <span className="btn-icon"><Ico n="list" /> Detection Rules</span>
        </button>
        <button type="button" className={aiView === 'classes' ? 'active' : 'quiet'} onClick={() => setAiView('classes')}>
          <span className="btn-icon"><Ico n="grid2" /> Object Classes</span>
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
                    <h2>{cameraTitle(selectedCamera)}</h2>
                    <p className="section-subtitle">{selectedCamera.host || selectedCamera.xAddr || 'Saved camera'}</p>
                  </div>
                  <span className="status-pill">{selectedRules.length} rules</span>
                </header>
                <form className="vision-rule-form" onSubmit={onSaveRule}>
                  <FormBusyOverlay busy={busy} />
                  <header>
                    <h2>{ruleDraft.id ? 'Edit Rule' : 'New Rule'}</h2>
                    {ruleDraft.id ? (
                      <button type="button" className="quiet" onClick={() => onRuleDraft(defaultVisionRuleDraft(selectedCamera.id))} disabled={busy}>
                        <span className="btn-icon"><Ico n="plus" /> New Rule</span>
                      </button>
                    ) : null}
                  </header>
                  <div className="metadata-row">
                    <label>
                      Rule name
                      <input
                        value={ruleDraft.name || ''}
                        onChange={(event) => onRuleDraft({ ...ruleDraft, name: event.target.value })}
                        placeholder={`${cameraTitle(selectedCamera)} fire watch`}
                      />
                    </label>
                    <label>
                      Mode (how)
                      <select value={mode} onChange={(event) => changeMode(event.target.value)}>
                        {detectionModes.map(([value, label]) => (
                          <option key={value} value={value}>{label}</option>
                        ))}
                      </select>
                    </label>
                  </div>
                  <section className="schedule-panel">
                    <header>
                      <h3>Detect (what)</h3>
                      <span className="status-pill">
                        {targetClasses.includes('*') ? 'anything' : `${targetClasses.length} selected`}
                      </span>
                    </header>
                    <span className="field-hint">Pick the object classes this rule watches. Manage classes and groups in Object Classes below.</span>
                    <div className="schedule-days">
                      <label className="check-row line-class-any">
                        <input
                          type="checkbox"
                          checked={targetClasses.includes('*')}
                          onChange={(event) => changeTargets(event.target.checked ? ['*'] : ['person'])}
                        />
                        <strong>Anything</strong> — any detected object
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
                                {!live ? <span className="class-inactive-tag"> · model not active</span> : null}
                              </label>
                            );
                          })}
                        </div>
                      ))}
                      {classGroups.length === 0 ? (
                        <span className="field-hint">No object classes yet — add one in Object Classes below.</span>
                      ) : null}
                    </div>
                  </section>
                  <div className="metadata-row">
                    <label>
                      Threshold
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
                      Min frames
                      <input
                        type="number"
                        min="1"
                        value={ruleDraft.minFrames}
                        onChange={(event) => onRuleDraft({ ...ruleDraft, minFrames: Number(event.target.value) })}
                      />
                    </label>
                  </div>
                  <label>
                    Cooldown seconds
                    <input
                      type="number"
                      min="0"
                      value={ruleDraft.cooldownSeconds}
                      onChange={(event) => onRuleDraft({ ...ruleDraft, cooldownSeconds: Number(event.target.value) })}
                    />
                  </label>
                  <section className="schedule-panel">
                    <header>
                      <h3>Schedule</h3>
                      <span className="status-pill">{scheduleSummary(ruleDraft.schedulePolicy)}</span>
                    </header>
                    <div className="metadata-row">
                      <label>
                        Detection schedule
                        <select value={scheduleDraft.preset} onChange={(event) => changeSchedulePreset(event.target.value)}>
                          <option value="always">Always active</option>
                          <option value="daytime">Daytime</option>
                          <option value="nighttime">Nighttime</option>
                          <option value="weekdays">Weekdays</option>
                          <option value="weekends">Weekends</option>
                          <option value="custom">Custom weekly</option>
                          <option value="range">Specific datetime</option>
                        </select>
                      </label>
                      {scheduleDraft.preset === 'custom' || scheduleDraft.preset === 'range' ? (
                        <label>
                          Policy mode
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
                            <option value="allow">Detect only during this schedule</option>
                            <option value="deny">Pause during this schedule</option>
                          </select>
                        </label>
                      ) : null}
                    </div>
                    {scheduleDraft.preset === 'custom' ? (
                      <>
                        <label>
                          Timezone
                          <input
                            value={scheduleDraft.timezone}
                            onChange={(event) => changeCustomSchedule({ timezone: event.target.value })}
                            placeholder="Asia/Kuala_Lumpur"
                            autoComplete="off"
                          />
                        </label>
                        <div className="schedule-edit-block">
                          <strong>Active days</strong>
                          <div className="schedule-days" aria-label="Schedule days">
                            {scheduleDayOptions.map(([day, label]) => (
                              <label className="check-row" key={day}>
                                <input
                                  type="checkbox"
                                  checked={scheduleDraft.days.includes(day)}
                                  onChange={() => toggleScheduleDay(day)}
                                />
                                {label}
                              </label>
                            ))}
                          </div>
                        </div>
                        <div className="metadata-row">
                          <label>
                            Start time (HH:MM)
                            <input
                              type="time"
                              value={scheduleDraft.start}
                              onChange={(event) => changeCustomSchedule({ start: event.target.value })}
                            />
                          </label>
                          <label>
                            End time (HH:MM)
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
                          Timezone
                          <input
                            value={scheduleDraft.timezone}
                            onChange={(event) => changeRangeSchedule({ timezone: event.target.value })}
                            placeholder="Asia/Kuala_Lumpur"
                            autoComplete="off"
                          />
                        </label>
                        <div className="metadata-row">
                          <label>
                            Start datetime
                            <input
                              type="datetime-local"
                              value={scheduleDraft.rangeStart}
                              onChange={(event) => changeRangeSchedule({ rangeStart: event.target.value })}
                            />
                          </label>
                          <label>
                            End datetime
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
                  {crowdRule ? (
                    <section className="schedule-panel">
                      <header>
                        <h3>Crowd Threshold</h3>
                        <span className="status-pill">{crowdRuleConfig.minCount}+ people</span>
                      </header>
                      <span className="field-hint">
                        Fires when at least this many people are detected inside the zone in a single frame.
                        Each person must meet the confidence threshold above.
                      </span>
                      <div className="metadata-row">
                        <label>
                          Minimum people
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
                  {lineRule ? (
                    <>
                      <section className="schedule-panel">
                        <header>
                          <h3>Line Direction</h3>
                          <span className="status-pill">{lineRuleConfig.direction}</span>
                        </header>
                        <div className="metadata-row">
                          <label>
                            Direction
                            <select value={lineRuleConfig.direction} onChange={(event) => changeLineConfig({ direction: event.target.value })}>
                              <option value="both">Either direction</option>
                              <option value="forward">Forward side</option>
                              <option value="reverse">Reverse side</option>
                            </select>
                          </label>
                          {mode === 'multi_line_crossing' ? (
                            <label>
                              Max seconds between lines
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
                      <LineDrawingPreview
                        camera={selectedCamera}
                        config={lineRuleConfig}
                        detectionType={ruleDraft.detectionType}
                        authHeader={authHeader}
                        streamConfig={streamConfig}
                        disabled={busy}
                        onConfig={changeLineConfig}
                      />
                    </>
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
                      Sound alert
                    </label>
                    <label className="check-row">
                      <input
                        type="checkbox"
                        checked={Boolean(ruleDraft.isEnabled)}
                        onChange={(event) => onRuleDraft({ ...ruleDraft, isEnabled: event.target.checked })}
                      />
                      Enabled
                    </label>
                  </div>
                  <div className="action-row">
                    <button type="submit" disabled={busy || targetClasses.length < 1 || (!lineRule && selectedZonePoints.length < 3) || (lineRule && lineRuleConfig.lines.length < (mode === 'multi_line_crossing' ? 2 : 1))}>
                      <span className="btn-icon"><Ico n="save" /> Save Rule</span>
                    </button>
                    <button
                      type="button"
                      className="quiet"
                      onClick={() => onRuleDraft(defaultVisionRuleDraft(selectedCamera.id))}
                      disabled={busy}
                    >
                      Clear
                    </button>
                  </div>
                </form>
              </section>

              <section className="settings-panel">
                <header>
                  <h2>Rules</h2>
                  <span className="status-pill">{selectedRules.length}</span>
                </header>
                <div className="vision-list">
                  {selectedRules.length === 0 ? <p className="empty">No AI detection rules for this camera.</p> : null}
                  {selectedRules.map((rule) => (
                    <article className="vision-row" key={rule.id}>
                      <div>
                        <h3>{rule.name || rule.detectionType}</h3>
                        <p>
                          {rule.detectionType} / threshold {Number(rule.threshold || 0).toFixed(2)}
                          {lineCountFromRule(rule) ? ` / ${lineCountFromRule(rule)}` : ''} / {scheduleSummary(rule.schedulePolicy)}
                        </p>
                      </div>
                      <strong className={`status-pill ${rule.isEnabled ? 'online' : 'unknown'}`}>
                        {rule.isEnabled ? 'enabled' : 'disabled'}
                      </strong>
                      <div className="action-row">
                        <button type="button" className="quiet" onClick={() => onEditRule(rule)} disabled={busy}>
                          <span className="btn-icon"><Ico n="edit-2" /> Edit</span>
                        </button>
                        <button type="button" onClick={() => onTriggerTestAlert(rule)} disabled={busy}>
                          <span className="btn-icon"><Ico n="play" /> Test Alert</span>
                        </button>
                        <button type="button" className="quiet danger-text" onClick={() => onDeleteRule(rule.id)} disabled={busy}>
                          <span className="btn-icon"><Ico n="trash" /> Delete</span>
                        </button>
                      </div>
                    </article>
                  ))}
                </div>
              </section>

              <section className="settings-panel">
                <header>
                  <h2>Alert Log</h2>
                  <span className="status-pill">{logLoading ? '…' : logTotal}</span>
                </header>
                <div className="vision-list">
                  <div className="log-toolbar" style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginBottom: '0.5rem', flexWrap: 'wrap' }}>
                    <label style={{ display: 'flex', gap: '0.4rem', alignItems: 'center', margin: 0 }}>
                      Date
                      <input
                        type="date"
                        value={logDate}
                        max={todayDateString()}
                        onChange={(e) => setLogDate(e.target.value)}
                      />
                    </label>
                    <label style={{ display: 'flex', gap: '0.4rem', alignItems: 'center', margin: 0 }}>
                      Status
                      <select value={logStatus} onChange={(e) => setLogStatus(e.target.value)}>
                        <option value="">All</option>
                        <option value="active">Active</option>
                        <option value="acknowledged">Acknowledged</option>
                        <option value="diagnostic">Diagnostic</option>
                      </select>
                    </label>
                    <label style={{ display: 'flex', gap: '0.4rem', alignItems: 'center', margin: 0 }}>
                      Rule
                      <select value={logRuleId} onChange={(e) => setLogRuleId(e.target.value)}>
                        <option value="">All rules</option>
                        {selectedRules.map((rule) => (
                          <option key={rule.id} value={rule.id}>{rule.name || rule.detectionType || `Rule #${rule.id}`}</option>
                        ))}
                      </select>
                    </label>
                    <button type="button" className="quiet" onClick={() => setLogDate('')} disabled={!logDate}>
                      All dates
                    </button>
                    <button type="button" className="quiet" onClick={() => setLogDate(todayDateString())} disabled={logDate === todayDateString()}>
                      Today
                    </button>
                    <button type="button" className="quiet" onClick={() => fetchLogAlerts(selectedCamera?.id, logPage, logDate, logStatus, logRuleId)} disabled={logLoading}>
                      Reload
                    </button>
                  </div>
                  {logLoading ? <p className="empty">Loading…</p> : null}
                  {!logLoading && logAlerts.length === 0 ? <p className="empty">No alert events for this camera{logDate ? ' on this date' : ''}.</p> : null}
                  {logAlerts.length > 0 ? (
                    <div className="event-table-wrap">
                      <table className="event-table">
                        <thead>
                          <tr>
                            <th>Time</th>
                            <th>Event</th>
                            <th>Rule</th>
                            <th>Confidence</th>
                            <th>Status</th>
                            <th>Action</th>
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
                                  <strong>{objectLabel || alert.label || alert.detectionType || 'Detection event'}</strong>
                                  <span>{formatSourceLabel(metadata.source)}</span>
                                </td>
                                <td>{rule?.name || `#${alert.ruleId || '-'}`}</td>
                                <td>{Number(alert.confidence || 0).toFixed(3)}</td>
                                <td>
                                  <span className={`status-pill ${diagnostic ? 'unknown' : alert.isAcknowledged ? 'resolved' : 'offline'}`}>
                                    {diagnostic ? 'diagnostic' : alert.isAcknowledged ? 'acknowledged' : 'active'}
                                  </span>
                                </td>
                                <td>
                                  <div className="table-actions">
                                    <button type="button" className="quiet" onClick={() => setLogSelectedAlertId(Number(logSelectedAlertId) === Number(alert.id) ? null : alert.id)}>
                                      {Number(logSelectedAlertId) === Number(alert.id) ? 'Close' : 'Details'}
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
                        ‹ Prev
                      </button>
                      <span style={{ fontSize: '0.85rem' }}>
                        Page {logPage + 1} / {Math.ceil(logTotal / logPageSize)}
                      </span>
                      <button type="button" className="quiet" onClick={() => setLogPage((p) => p + 1)} disabled={(logPage + 1) * logPageSize >= logTotal || logLoading}>
                        Next ›
                      </button>
                    </div>
                  ) : null}
                  {null /* detail shown in AlertDetailModal overlay */}
                </div>
              </section>
            </>
          ) : (
            <section className="device-card empty-detail">
              <h2>No saved camera selected</h2>
              <p className="empty">Save a camera first, then create AI detection rules here.</p>
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
const kindLabels = { object: 'Object category', hazard: 'Hazard category', group: 'Group of categories' };

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
        <h2>Object Classes</h2>
        <span className="status-pill">{(classes || []).length}</span>
      </header>
      <span className="field-hint">
        These are the <strong>targets</strong> you pick in a rule's <strong>Detect</strong> list. A <strong>category</strong>
        {' '}(e.g. Person, Vehicle) maps a friendly name to the raw labels your model outputs. A <strong>group</strong> bundles
        several categories. <strong>Tip:</strong> to file a trained label like <code>papa</code> under an existing category,
        click <strong>Edit</strong> on that category and add the label there — it won't appear as its own top-level class.
      </span>

      <div className="action-row">
        <button type="button" onClick={openNew} disabled={busy}>
          <span className="btn-icon"><Ico n="plus" /> Add category or group</span>
        </button>
      </div>

      <ul className="class-registry-list">
        {(classes || []).map((cls) => (
          <li key={cls.id} className="class-registry-row">
            <div className="class-registry-info">
              <strong>{cls.displayName || cls.name}</strong>
              <span className={`class-source-badge ${cls.source}`}>{cls.source}</span>
              {cls.source === 'trained' && !classIsLive(cls, activeModelClasses) ? (
                <span className="class-source-badge inactive">model not active</span>
              ) : null}
              <div className="chip-list">
                {(cls.memberList || []).length
                  ? (cls.memberList || []).map((m) => <span key={m} className="chip chip-static">{m}</span>)
                  : <span className="field-hint">no labels</span>}
              </div>
            </div>
            <div className="class-registry-actions">
              <button type="button" className="quiet" onClick={() => editClass(cls)} disabled={busy}>Edit</button>
              {cls.source !== 'builtin' ? (
                <button type="button" className="quiet danger" onClick={() => onDeleteClass(cls.id)} disabled={busy}>Delete</button>
              ) : null}
            </div>
          </li>
        ))}
      </ul>

      {editorOpen ? (
      <div className="video-overlay" onClick={closeEditor}>
      <div className="video-dialog class-editor-dialog" onClick={(e) => e.stopPropagation()}>
        <div className="video-dialog-header">
          <span className="video-dialog-title">{isNew ? 'New category or group' : `Edit "${draft.displayName || draft.name}"`}</span>
          <button type="button" className="video-dialog-close" onClick={closeEditor} aria-label="Close">✕</button>
        </div>
      <form className="vision-rule-form class-editor-form" onSubmit={submit}>
        <div className="metadata-row">
          <label>
            Name
            <input
              value={draft.displayName}
              onChange={(event) => setDraft({ ...draft, displayName: event.target.value })}
              placeholder="e.g. Person, Delivery"
              disabled={busy || draft.source === 'builtin'}
            />
            {!isNew ? <span className="field-hint">id: {draft.name}</span> : null}
          </label>
          <label>
            Type
            <select
              value={draft.kind}
              onChange={(event) => setDraft({ ...draft, kind: event.target.value })}
              disabled={busy || draft.source === 'builtin'}
            >
              <option value="object">{kindLabels.object}</option>
              <option value="hazard">{kindLabels.hazard}</option>
              <option value="group">{kindLabels.group}</option>
            </select>
          </label>
        </div>

        {isGroup ? (
          <div className="class-member-editor">
            <strong>Categories in this group</strong>
            <span className="field-hint">A rule targeting this group matches any of the selected categories.</span>
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
              {selectableMembers.length === 0 ? <span className="field-hint">No categories to group yet — create some first.</span> : null}
            </div>
          </div>
        ) : (
          <div className="class-member-editor">
            <strong>Model labels in this category</strong>
            <span className="field-hint">
              The raw labels your models output. A rule targeting this category matches any of them (e.g. Vehicle = car, truck, bus).
              <strong> Search and pick from the list</strong> — a label only detects something if a model that produces it is active, and a
              mistyped name never matches. New trained labels (from the <strong>Training</strong> tab) appear here once their model is active.
            </span>

            {draft.members.length ? (
              <div className="chip-list">
                {draft.members.map((label) => (
                  <span key={label} className="chip">
                    {label}
                    <button type="button" className="chip-remove" onClick={() => removeLabel(label)} aria-label={`Remove ${label}`} disabled={busy}>×</button>
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
                placeholder={catalog.length ? `Search ${catalog.length} labels, or type a new one…` : 'Type a label…'}
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
                              {entry.source === 'trained' ? <span className="chip-tag">trained</span> : null}
                            </button>
                          ))}
                          {entries.length > shown.length ? (
                            <span className="field-hint">+{entries.length - shown.length} more — search to narrow.</span>
                          ) : null}
                        </div>
                      ) : null}
                    </div>
                  );
                })}
                {availableEntries.length === 0
                  ? <span className="field-hint">Every known label is already in this category.</span> : null}
                {availableEntries.length > 0 && matchingEntries.length === 0 && !customAddable
                  ? <span className="field-hint">No known labels match.</span> : null}
              </div>
              {customAddable ? (
                <button type="button" className="quiet" onClick={() => addLabel(labelInput)} disabled={busy}>
                  <span className="btn-icon"><Ico n="plus" /> Add “{labelInput.trim()}” as a custom label</span>
                </button>
              ) : null}
            </div>
          </div>
        )}

        <div className="action-row">
          <button type="submit" disabled={busy || !draft.displayName.trim()}>
            <span className="btn-icon"><Ico n="save" /> Save</span>
          </button>
          <button type="button" className="quiet" onClick={closeEditor} disabled={busy}>Cancel</button>
        </div>
      </form>
      </div>
      </div>
      ) : null}
    </section>
  );
}

export function AlertDetailModal({ alert, rule, authHeader, onClose }) {
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

  const title = meta.objectLabel || alert.label || alert.detectionType || 'Detection event';

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
              {isDiagnostic ? 'diagnostic' : alert.isAcknowledged ? 'acknowledged' : 'active'}
            </span>
            <span className="alert-modal-time">{formatTimestamp(alert.createdAt)}</span>
          </div>
          <button type="button" className="alert-modal-close" onClick={onClose} aria-label="Close">✕</button>
        </div>

        <div className="alert-modal-image-wrap">
          {snapLoading && <div className="alert-modal-snap-msg">Loading snapshot…</div>}
          {snapError && !snapLoading && <div className="alert-modal-snap-msg alert-modal-snap-none">No snapshot available</div>}
          {snapUrl && (
            <div className="alert-modal-snap-container">
              <img className="alert-modal-snap" src={snapUrl} alt="Detection snapshot" />
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
                <span className="btn-icon"><Ico n="download" /> Download with box</span>
              </button>
            </div>
          )}
        </div>

        <div className="alert-modal-meta">
          <dl className="event-grid">
            <div><dt>Rule</dt><dd>{rule?.name || `#${alert.ruleId || '-'}`}</dd></div>
            <div><dt>Type</dt><dd>{fieldValue(alert.detectionType)}</dd></div>
            <div><dt>Source</dt><dd>{formatSourceLabel(meta.source)}</dd></div>
            <div>
              <dt>Confidence</dt>
              <dd>
                <span className="conf-value">{conf.toFixed(3)}</span>
                <div className="conf-bar-wrap">
                  <div className="conf-bar" style={{ width: `${(conf * 100).toFixed(1)}%`, background: conf >= 0.6 ? 'var(--accent)' : conf >= 0.35 ? 'var(--warn-color, #e8a000)' : 'var(--danger-color, #d9534f)' }} />
                </div>
              </dd>
            </div>
            {isYolo && meta.objectLabel ? <div><dt>Object Label</dt><dd style={{ textTransform: 'capitalize' }}>{meta.objectLabel}</dd></div> : null}
            {trackId ? <div><dt>Track ID</dt><dd>{trackId}</dd></div> : null}
            {isLineCrossing && meta.lineId ? (
              <div><dt>Line</dt><dd>{meta.lineId}{meta.lineCount > 1 ? ` (${Number(meta.lineIndex) + 1}/${meta.lineCount})` : ''}</dd></div>
            ) : null}
            {isMotion && meta.changedRatio !== undefined ? <div><dt>Changed Area</dt><dd>{formatPercent(meta.changedRatio)}</dd></div> : null}
            {isDiagnostic ? (
              <>
                <div><dt>Status</dt><dd>{fieldValue(meta.status)}</dd></div>
                <div><dt>Threshold</dt><dd>{meta.ruleThreshold !== undefined ? Number(meta.ruleThreshold).toFixed(2) : '-'}</dd></div>
                <div><dt>Min Frames</dt><dd>{meta.ruleMinFrames ?? '-'}</dd></div>
                <div><dt>Cooldown</dt><dd>{meta.ruleCooldownSec !== undefined ? `${meta.ruleCooldownSec}s` : '-'}</dd></div>
                {meta.message ? <div style={{ gridColumn: '1 / -1' }}><dt>Message</dt><dd>{meta.message}</dd></div> : null}
              </>
            ) : null}
            {bb ? <div><dt>Bounding Box</dt><dd className="bb-coords-inline">X {formatPercent(bb.x)} Y {formatPercent(bb.y)} W {formatPercent(bb.w)} H {formatPercent(bb.h)}</dd></div> : null}
            <div><dt>Acknowledged</dt><dd>{alert.isAcknowledged ? formatTimestamp(alert.acknowledgedAt) : '-'}</dd></div>
          </dl>
        </div>
      </div>
    </div>
  );
}

