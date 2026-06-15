import { useState, useEffect, useMemo, useCallback } from 'react';
import { Ico } from './icons';
import { FormBusyOverlay } from './ui';
import { useSnapshotBlob } from '../hooks';
import { lineClassOptions, defaultLineClasses, scheduleDayOptions } from '../lib/constants';
import {todayDateString,apiBase,fieldValue,formatTimestamp,parseMetadata,formatPercent,parseBoundingBox,formatSourceLabel,cameraTitle,orderedSavedCameras,parseZonePolygon,defaultZonePolygon,isLineDetectionType,normalizeLineConfig,parseLineRuleConfig,lineRuleConfigText,lineCountFromRule,defaultVisionRuleDraft,weeklySchedulePolicy,rangeSchedulePolicy,schedulePresetPolicy,scheduleDraftFromPolicy,scheduleSummary } from '../lib/helpers';
import { ZoneDrawingPreview, LineDrawingPreview } from './previews';
import { SavedDeviceNav } from './cameras';

export function VisionTab({
  saved,
  rules,
  alerts,
  ruleDraft,
  busy,
  authHeader,
  streamConfig,
  onRuleDraft,
  onSaveRule,
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
  const lineRule = isLineDetectionType(ruleDraft.detectionType);
  const lineRuleConfig = parseLineRuleConfig(ruleDraft.ruleConfig, ruleDraft.detectionType);
  const selectedZonePoints = parseZonePolygon(ruleDraft.zonePolygon);
  const scheduleDraft = scheduleDraftFromPolicy(ruleDraft.schedulePolicy);
  const [logSelectedAlertId, setLogSelectedAlertId] = useState(null);

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

  function changeDetectionType(detectionType) {
    const next = { ...ruleDraft, detectionType };
    if (isLineDetectionType(detectionType)) {
      next.ruleConfig = lineRuleConfigText(parseLineRuleConfig(ruleDraft.ruleConfig, detectionType), detectionType);
      next.zonePolygon = ruleDraft.zonePolygon || defaultZonePolygon;
    } else {
      next.ruleConfig = '';
    }
    onRuleDraft(next);
  }

  function changeLineConfig(patch) {
    const next = normalizeLineConfig({ ...lineRuleConfig, ...patch }, ruleDraft.detectionType);
    onRuleDraft({ ...ruleDraft, ruleConfig: lineRuleConfigText(next, ruleDraft.detectionType) });
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
          <p className="section-subtitle">Camera rules and alert events.</p>
        </div>
        <button type="button" className="quiet" onClick={onReload} disabled={busy}>
          <span className="btn-icon"><Ico n="reload" /> Reload</span>
        </button>
      </div>

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
                      Detection type
                      <select
                        value={ruleDraft.detectionType}
                        onChange={(event) => changeDetectionType(event.target.value)}
                      >
                        <option value="fire">Fire</option>
                        <option value="smoke">Smoke</option>
                        <option value="person">Person</option>
                        <option value="vehicle">Vehicle</option>
                        <option value="animal">Animal</option>
                        <option value="intrusion">Intrusion</option>
                        <option value="line_crossing">Line crossing</option>
                        <option value="multi_line_crossing">Multi-line crossing</option>
                      </select>
                    </label>
                  </div>
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
                  {lineRule ? (
                    <>
                      <section className="schedule-panel">
                        <header>
                          <h3>Object Classes</h3>
                          <span className="status-pill">
                            {lineRuleConfig.classes.includes('*') ? 'any' : lineRuleConfig.classes.length}
                          </span>
                        </header>
                        <div className="schedule-days">
                          <label className="check-row line-class-any">
                            <input
                              type="checkbox"
                              checked={lineRuleConfig.classes.includes('*')}
                              onChange={(event) => {
                                changeLineConfig({ classes: event.target.checked ? ['*'] : defaultLineClasses });
                              }}
                            />
                            <strong>Anything</strong> — any object
                          </label>
                          {!lineRuleConfig.classes.includes('*') && lineClassOptions.map((label) => (
                            <label className="check-row" key={label}>
                              <input
                                type="checkbox"
                                checked={lineRuleConfig.classes.includes(label)}
                                onChange={(event) => {
                                  const current = new Set(lineRuleConfig.classes);
                                  if (event.target.checked) {
                                    current.add(label);
                                  } else {
                                    current.delete(label);
                                  }
                                  changeLineConfig({ classes: Array.from(current) });
                                }}
                              />
                              {label}
                            </label>
                          ))}
                        </div>
                        <div className="metadata-row">
                          <label>
                            Direction
                            <select value={lineRuleConfig.direction} onChange={(event) => changeLineConfig({ direction: event.target.value })}>
                              <option value="both">Either direction</option>
                              <option value="forward">Forward side</option>
                              <option value="reverse">Reverse side</option>
                            </select>
                          </label>
                          {ruleDraft.detectionType === 'multi_line_crossing' ? (
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
                    <button type="submit" disabled={busy || (!lineRule && selectedZonePoints.length < 3) || (lineRule && lineRuleConfig.lines.length < (ruleDraft.detectionType === 'multi_line_crossing' ? 2 : 1))}>
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

