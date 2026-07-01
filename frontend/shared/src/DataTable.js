import { useEffect, useMemo, useRef, useState } from 'react';
import { useT } from './i18n';

// DataTable is the shared filterable/sortable/pageable data grid for the RBAC apps.
// Columns are `{ key, label, render?, filterable?, filterType? }`; action columns set
// `filterable:false`. The CSS classes it renders (.table-surface, .pager, .filter-*,
// .sort-button, …) are styled by the consuming app's stylesheet.
//
// Two modes:
//   - client (default): filtering/sorting/paging run in-memory over `rows`.
//   - server (`serverMode`): the grid is a controlled view — `rows` is already the
//     current page, `total` is the full filtered count, and `onQuery({ filters,
//     sorters, offset, limit })` fires whenever the filter/sort/page state changes so
//     the parent can run the query in the DB. `filters`/`sorters` use the same shape
//     the backend query parser expects ({ fieldName, compare, value } / { fieldName,
//     sort }), so they can be JSON-stringified straight into the request.

const FILTER_OPERATORS = [
  { value: 1, label: '=' },
  { value: 2, label: '!=' },
  { value: 3, label: '>' },
  { value: 4, label: '<' },
  { value: 5, label: '>=' },
  { value: 6, label: '<=' },
];
const TEXT_FILTER_OPERATORS = FILTER_OPERATORS.filter((op) => [1, 2].includes(op.value));
const BOOLEAN_FILTER_OPERATORS = FILTER_OPERATORS.filter((op) => [1, 2].includes(op.value));

export function DataTable({ rows, columns, pageSize: initialPageSize = 10, pageSizeOptions = null, busy = false, emptyText, serverMode = false, total, onQuery, initialFilters = null }) {
  const t = useT();
  // `initialFilters` seeds the column filters at mount (shape: { colKey: [{compare,
  // value}] }). Remount (via a `key`) to apply a fresh seed — e.g. a "Today" preset.
  const [columnFilters, setColumnFilters] = useState(() => (initialFilters && typeof initialFilters === 'object' ? { ...initialFilters } : {}));
  const [sorters, setSorters] = useState([]);
  const [offset, setOffset] = useState(0);
  const [openFilter, setOpenFilter] = useState(null);
  // Page size is stateful when a selector is offered (pageSizeOptions), otherwise it
  // is simply the fixed prop. Changing it returns to the first page.
  const [pageSize, setPageSize] = useState(initialPageSize);
  function changePageSize(n) {
    setPageSize(n);
    setOffset(0);
  }

  // Filter/sort metadata is derived only for the data columns (action columns opt
  // out with filterable:false); every column is still rendered.
  const fieldColumns = useMemo(
    () => columns
      .filter((c) => c.filterable !== false)
      .map((c) => ({ ...c, filterType: c.filterType || inferFilterType(c.key) })),
    [columns],
  );

  const filters = useMemo(
    () => fieldColumns.flatMap((column) => normalizeFilterDrafts(columnFilters[column.key], column)
      .map((filter) => {
        const value = String(filter?.value ?? '').trim();
        if (value === '') return null;
        const compare = normalizeFilterCompare(filter?.compare, column);
        return {
          fieldName: column.key,
          compare,
          value: coerceFilterValue(value, column, compare),
        };
      })
      .filter(Boolean)),
    [columnFilters, fieldColumns],
  );

  // Client mode filters/sorts in-memory; server mode trusts `rows` as the ready page.
  const clientViewRows = useMemo(
    () => applyClientSorters(applyClientFilters(Array.isArray(rows) ? rows : [], filters, fieldColumns), sorters, fieldColumns),
    [rows, filters, sorters, fieldColumns],
  );
  const viewRows = serverMode ? (Array.isArray(rows) ? rows : []) : clientViewRows;

  // Server mode: emit the current query whenever filters/sorters/page change, always
  // calling the latest onQuery (via a ref) so an inline parent callback never causes a
  // refetch loop. The `filters` array is re-derived every render (callers pass inline
  // `columns`/`rows`), so guard on a serialized signature — only emit when the query
  // actually changed, otherwise a fetch → setState → re-render would loop forever.
  const onQueryRef = useRef(onQuery);
  useEffect(() => { onQueryRef.current = onQuery; });
  const lastQuerySigRef = useRef(null);
  useEffect(() => {
    if (!serverMode) return;
    const query = { filters, sorters, offset, limit: pageSize };
    const sig = JSON.stringify(query);
    if (sig === lastQuerySigRef.current) return;
    lastQuerySigRef.current = sig;
    if (onQueryRef.current) onQueryRef.current(query);
  }, [serverMode, filters, sorters, offset, pageSize]);

  // Client mode only: keep the page offset in range as filtering shrinks the set.
  useEffect(() => {
    if (!serverMode && offset > 0 && offset >= clientViewRows.length) setOffset(0);
  }, [serverMode, clientViewRows.length, offset]);

  const pageRows = serverMode ? viewRows : viewRows.slice(offset, offset + pageSize);
  const pagerTotal = serverMode ? (Number(total) || 0) : viewRows.length;

  function updateColumnFilter(fieldName, criteria) {
    setColumnFilters((current) => {
      const column = fieldColumns.find((item) => item.key === fieldName);
      if (!column) return current;
      const next = normalizeFilterDrafts(criteria, column).filter((f) => String(f.value ?? '').trim() !== '');
      if (next.length === 0) {
        const { [fieldName]: _removed, ...rest } = current;
        return rest;
      }
      return { ...current, [fieldName]: next };
    });
    setOffset(0);
  }

  function toggleSorter(fieldName) {
    setSorters((current) => {
      const existing = current.find((s) => s.fieldName === fieldName);
      if (!existing) return [...current, { fieldName, sort: 1 }];
      if (Number(existing.sort) === 1) return current.map((s) => (s.fieldName === fieldName ? { ...s, sort: 2 } : s));
      return current.filter((s) => s.fieldName !== fieldName);
    });
    // Re-sorting returns to the first page (matches filtering; keeps server paging sane).
    setOffset(0);
  }

  function openColumnFilter(column, anchor) {
    const rect = anchor.getBoundingClientRect();
    setOpenFilter({
      column,
      left: Math.max(12, Math.min(rect.left, window.innerWidth - 300)),
      top: rect.bottom + 8,
    });
  }

  return (
    <div className={busy ? 'table-surface table-loading' : 'table-surface'} aria-busy={busy}>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              {columns.map((column) => (
                <th key={column.key}>
                  {column.filterable === false ? (
                    <div className="column-head"><div className="column-title-row"><span>{column.label}</span></div></div>
                  ) : (
                    <ColumnHeader
                      column={fieldColumns.find((c) => c.key === column.key) || column}
                      filter={columnFilters[column.key]}
                      sort={sortWithIndex(sorters, column.key)}
                      onFilterOpen={openColumnFilter}
                      onSort={toggleSorter}
                    />
                  )}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {!busy && pageRows.length === 0 && (
              <tr><td className="empty-cell" colSpan={columns.length}>{emptyText != null ? emptyText : t('table.empty')}</td></tr>
            )}
            {pageRows.map((row, idx) => (
              <tr key={row.id ?? idx}>
                {columns.map((column) => (
                  <td key={column.key}>{column.render ? column.render(row[column.key], row) : printableCell(row[column.key], t)}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager total={pagerTotal} offset={offset} limit={pageSize} onPage={setOffset} busy={busy} pageSizeOptions={pageSizeOptions} onPageSize={changePageSize} />
      {openFilter && (
        <>
          <button className="filter-popover-backdrop" onClick={() => setOpenFilter(null)} type="button" aria-label="Close filter" />
          <ColumnFilterPopover
            column={openFilter.column}
            filter={columnFilters[openFilter.column.key]}
            left={openFilter.left}
            top={openFilter.top}
            onApply={(fieldName, patch) => { updateColumnFilter(fieldName, patch); setOpenFilter(null); }}
            onClear={() => { updateColumnFilter(openFilter.column.key, []); setOpenFilter(null); }}
            onClose={() => setOpenFilter(null)}
          />
        </>
      )}
    </div>
  );
}

function ColumnHeader({ column, filter, sort, onFilterOpen, onSort }) {
  const t = useT();
  const filterCount = normalizeFilterDrafts(filter, column).filter((item) => String(item.value ?? '').trim() !== '').length;
  const sortLabel = sort?.sort === 1 ? t('table.sortAsc') : sort?.sort === 2 ? t('table.sortDesc') : t('table.sort');

  return (
    <div className="column-head">
      <div className="column-title-row">
        <span>{column.label}</span>
        <div className="column-actions">
          <button
            aria-label={t('table.filterCol', { col: column.label })}
            className={filterCount > 0 ? 'filter-button active' : 'filter-button'}
            onClick={(event) => onFilterOpen(column, event.currentTarget)}
            title={t('table.filterCol', { col: column.label })}
            type="button"
          >
            {filterCount > 1 && <span className="filter-count">{filterCount}</span>}
          </button>
          <button className={sort ? 'sort-button active' : 'sort-button'} onClick={() => onSort(column.key)} type="button" title={t('table.sortByCol', { col: column.label })}>
            {sortLabel}
            {sort?.index && <span>{sort.index}</span>}
          </button>
        </div>
      </div>
    </div>
  );
}

function ColumnFilterPopover({ column, filter, left, top, onApply, onClear, onClose }) {
  const t = useT();
  const isRange = column.filterType === 'daterange';
  const [draft, setDraft] = useState(() => normalizeFilterDrafts(filter, column));
  const [range, setRange] = useState(() => rangeFromFilter(filter));
  const operators = filterOperatorsForField(column);

  useEffect(() => {
    setDraft(normalizeFilterDrafts(filter, column));
    setRange(rangeFromFilter(filter));
  }, [column, filter]);

  function updateDraft(index, patch) {
    setDraft((current) => current.map((item, i) => (i === index
      ? { ...item, ...patch, compare: normalizeFilterCompare(patch.compare ?? item.compare, column) }
      : item)));
  }

  // A date-range filter picks a From/To span and emits it as two conditions
  // (createdAt >= from, createdAt <= to); either bound may be left open.
  function applyRange(e) {
    e.preventDefault();
    const conditions = [];
    if (range.from) conditions.push({ compare: 5, value: range.from });
    if (range.to) conditions.push({ compare: 6, value: range.to });
    onApply(column.key, conditions);
  }

  if (isRange) {
    return (
      <div className="filter-popover" style={{ left, top }}>
        <div className="filter-popover-head">
          <span>{column.label}</span>
          <button className="mini-button" onClick={onClose} type="button">{t('common.close')}</button>
        </div>
        <form className="filter-popover-body" onSubmit={applyRange}>
          <div className="filter-condition">
            <label>
              {t('table.dateFrom')}
              <input type="date" autoFocus value={range.from} max={range.to || undefined} onChange={(e) => setRange((r) => ({ ...r, from: e.target.value }))} />
            </label>
            <label>
              {t('table.dateTo')}
              <input type="date" value={range.to} min={range.from || undefined} onChange={(e) => setRange((r) => ({ ...r, to: e.target.value }))} />
            </label>
          </div>
          <div className="filter-popover-actions">
            <button className="secondary-button" onClick={() => setRange({ from: '', to: '' })} type="button">{t('common.clear')}</button>
            <button className="primary-button" type="submit">{t('common.apply')}</button>
          </div>
        </form>
      </div>
    );
  }

  return (
    <div className="filter-popover" style={{ left, top }}>
      <div className="filter-popover-head">
        <span>{column.label}</span>
        <button className="mini-button" onClick={onClose} type="button">{t('common.close')}</button>
      </div>
      <form className="filter-popover-body" onSubmit={(e) => { e.preventDefault(); onApply(column.key, draft); }}>
        {draft.map((item, index) => (
          <div className="filter-condition" key={`filter-${column.key}-${index}`}>
            <div className="filter-condition-head">
              <span>{t('table.condition', { n: index + 1 })}</span>
              {draft.length > 1 && <button className="mini-button" onClick={() => setDraft((c) => c.filter((_, i) => i !== index))} type="button">{t('common.remove')}</button>}
            </div>
            <label>
              {t('table.operator')}
              <select value={normalizeFilterCompare(item.compare, column)} onChange={(e) => updateDraft(index, { compare: Number(e.target.value) })}>
                {operators.map((op) => <option key={op.value} value={op.value}>{op.label}</option>)}
              </select>
            </label>
            <label>
              {t('table.value')}
              {column.filterType === 'boolean' ? (
                <select value={item.value} onChange={(e) => updateDraft(index, { value: e.target.value })}>
                  <option value="">{t('common.any')}</option>
                  <option value="true">{t('common.yes')}</option>
                  <option value="false">{t('common.no')}</option>
                </select>
              ) : (
                <input
                  autoFocus={index === 0}
                  value={item.value}
                  onChange={(e) => updateDraft(index, { value: e.target.value })}
                  type={column.filterType === 'number' ? 'number' : 'text'}
                />
              )}
            </label>
          </div>
        ))}
        <div className="filter-popover-actions">
          <button className="secondary-button" onClick={() => setDraft((c) => [...c, createColumnFilter(column)])} type="button">{t('common.add')}</button>
          <button className="secondary-button" onClick={onClear} type="button">{t('common.clear')}</button>
          <button className="primary-button" type="submit">{t('common.apply')}</button>
        </div>
      </form>
    </div>
  );
}

function Pager({ total, offset, limit, onPage, busy, pageSizeOptions, onPageSize }) {
  const t = useT();
  const showSize = Array.isArray(pageSizeOptions) && pageSizeOptions.length > 0 && typeof onPageSize === 'function';
  const pageCount = Math.max(1, Math.ceil(total / limit));
  const currentPage = Math.min(pageCount, Math.floor(offset / limit) + 1);
  const last = Math.max(0, (pageCount - 1) * limit);
  const [pageDraft, setPageDraft] = useState(String(currentPage));
  useEffect(() => { setPageDraft(String(currentPage)); }, [currentPage]);

  function goToDraftPage() {
    const target = Math.min(pageCount, Math.max(1, Number(pageDraft) || 1));
    onPage((target - 1) * limit);
  }

  return (
    <div className="pager">
      <div className="pager-meta">
        <span>{t('pager.summary', { current: currentPage, count: pageCount, total })}</span>
        {showSize ? (
          <label className="pager-size">
            {t('pager.rows')}
            <select value={limit} onChange={(e) => onPageSize(Number(e.target.value))} disabled={busy}>
              {pageSizeOptions.map((n) => <option key={n} value={n}>{n}</option>)}
            </select>
          </label>
        ) : null}
      </div>
      <div className="pager-controls">
        <button className="pager-icon first" disabled={busy || offset <= 0} onClick={() => onPage(0)} title={t('pager.first')} type="button" aria-label={t('pager.first')} />
        <button className="pager-icon previous" disabled={busy || offset <= 0} onClick={() => onPage(Math.max(0, offset - limit))} title={t('pager.previous')} type="button" aria-label={t('pager.previous')} />
        <label className="pager-jump">
          <input min="1" max={pageCount} value={pageDraft} onChange={(e) => setPageDraft(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); goToDraftPage(); } }} title={t('pager.pageNumber')} type="number" />
        </label>
        <button className="pager-icon go" disabled={busy} onClick={goToDraftPage} title={t('pager.goto')} type="button" aria-label={t('pager.goto')} />
        <button className="pager-icon next" disabled={busy || offset + limit >= total} onClick={() => onPage(offset + limit)} title={t('pager.next')} type="button" aria-label={t('pager.next')} />
        <button className="pager-icon last" disabled={busy || offset >= last} onClick={() => onPage(last)} title={t('pager.last')} type="button" aria-label={t('pager.last')} />
      </div>
    </div>
  );
}

// --- shared filter/sort helpers -------------------------------------------------

function createColumnFilter(field) {
  return { compare: filterOperatorsForField(field)[0]?.value || 1, value: '' };
}

function normalizeFilterDrafts(value, field) {
  const source = Array.isArray(value) ? value : value ? [value] : [createColumnFilter(field)];
  return source.map((item) => ({ compare: normalizeFilterCompare(item?.compare, field), value: item?.value ?? '' }));
}

function sortWithIndex(sorters, fieldName) {
  const index = sorters.findIndex((s) => s.fieldName === fieldName);
  return index >= 0 ? { ...sorters[index], index: index + 1 } : null;
}

function inferFilterType(key) {
  if (['isActive', 'disabled', 'isStock', 'isSuperadmin', 'builtin', 'canGet', 'canPost', 'canPut', 'canDelete'].includes(key)) return 'boolean';
  if (key === 'id' || key.endsWith('Id') || ['createdAt', 'updatedAt'].includes(key)) return 'number';
  return 'text';
}

function filterOperatorsForField(field) {
  if (field?.filterType === 'boolean') return BOOLEAN_FILTER_OPERATORS;
  if (field?.filterType === 'number' || field?.filterType === 'date' || field?.filterType === 'daterange') return FILTER_OPERATORS;
  return TEXT_FILTER_OPERATORS;
}

// rangeFromFilter reads a date-range column's stored conditions back into { from, to }
// date strings (compare >= / > is the lower bound, <= / < the upper) so re-opening the
// popover shows the picked span rather than raw epochs.
function rangeFromFilter(filter) {
  const list = Array.isArray(filter) ? filter : filter ? [filter] : [];
  let from = '';
  let to = '';
  for (const f of list) {
    const c = Number(f?.compare);
    if (c === 5 || c === 3) from = String(f?.value ?? '');
    else if (c === 6 || c === 4) to = String(f?.value ?? '');
  }
  return { from, to };
}

// dateToEpoch converts a YYYY-MM-DD picker value to a Unix timestamp (seconds). Upper
// bounds ('<=' and '>') snap to the END of the chosen day so the whole calendar day is
// covered; lower bounds use its start.
function dateToEpoch(value, compare) {
  const d = new Date(`${value}T00:00:00`);
  if (Number.isNaN(d.getTime())) return value;
  if (compare === 3 || compare === 6) d.setHours(23, 59, 59, 999);
  return Math.floor(d.getTime() / 1000);
}

function normalizeFilterCompare(compare, field) {
  const value = Number(compare || 1);
  const operators = filterOperatorsForField(field);
  return operators.some((op) => op.value === value) ? value : operators[0].value;
}

function coerceFilterValue(value, field, compare) {
  if (field?.filterType === 'boolean') return String(value).toLowerCase() === 'true';
  if (field?.filterType === 'number') return Number(value);
  if (field?.filterType === 'date' || field?.filterType === 'daterange') return dateToEpoch(value, compare);
  return value;
}

function applyClientFilters(rows, filters, fields) {
  if (!Array.isArray(rows) || filters.length === 0) return rows;
  return rows.filter((row) => filters.every((filter) => {
    const field = fields.find((item) => item.key === filter.fieldName);
    return compareFilterValue(row?.[filter.fieldName], filter.value, Number(filter.compare), field);
  }));
}

function applyClientSorters(rows, sorters, fields) {
  if (!Array.isArray(rows) || sorters.length === 0) return rows;
  const active = sorters.map((s) => ({ ...s, field: fields.find((item) => item.key === s.fieldName) })).filter((s) => s.field);
  if (active.length === 0) return rows;
  return [...rows].sort((a, b) => {
    for (const sorter of active) {
      const left = sortComparableValue(a?.[sorter.fieldName], sorter.field);
      const right = sortComparableValue(b?.[sorter.fieldName], sorter.field);
      if (left === right) continue;
      const direction = Number(sorter.sort) === 2 ? -1 : 1;
      return left > right ? direction : -direction;
    }
    return 0;
  });
}

function sortComparableValue(value, field) {
  if (field?.filterType === 'number' || field?.filterType === 'date' || field?.filterType === 'daterange') { const n = Number(value); return Number.isNaN(n) ? 0 : n; }
  if (field?.filterType === 'boolean') return value ? 1 : 0;
  return String(value ?? '').toLowerCase();
}

function compareFilterValue(actual, expected, compare, field) {
  if (field?.filterType === 'number' || field?.filterType === 'date' || field?.filterType === 'daterange') {
    const left = Number(actual);
    const right = Number(expected);
    if (Number.isNaN(left) || Number.isNaN(right)) return false;
    return compareValues(left, right, compare);
  }
  if (field?.filterType === 'boolean') return compareValues(Boolean(actual), Boolean(expected), compare);
  return compareValues(String(actual ?? '').toLowerCase(), String(expected ?? '').toLowerCase(), compare);
}

function compareValues(left, right, compare) {
  switch (compare) {
    case 2: return left !== right;
    case 3: return left > right;
    case 4: return left < right;
    case 5: return left >= right;
    case 6: return left <= right;
    case 1:
    default: return left === right;
  }
}

export function printable(value) {
  if (typeof value === 'boolean') return value ? 'Yes' : 'No';
  if (typeof value === 'object' && value !== null) return value.String || JSON.stringify(value);
  return value ?? '';
}

// printableCell is printable() with locale-aware booleans, used for default cell
// rendering inside the table (where a translate fn is available). printable() stays
// English-only for callers outside React (it can't use hooks).
function printableCell(value, t) {
  if (typeof value === 'boolean') return t(value ? 'common.yes' : 'common.no');
  return printable(value);
}
