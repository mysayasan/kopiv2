import { useEffect, useMemo, useState } from 'react';
import { Ico } from './icons';

// DataTable is myidsan's filterable/sortable data grid, ported into myseliasan so the
// two RBAC apps share one table design (column filter popovers, multi-sort, pager).
// myseliasan's admin endpoints return plain arrays, so filtering/sorting/paging run
// client-side here (myidsan does the same in its non-paging "default" list mode).

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

export function DataTable({ rows, columns, pageSize = 10, busy = false, emptyText = 'No records' }) {
  const [columnFilters, setColumnFilters] = useState({});
  const [sorters, setSorters] = useState([]);
  const [offset, setOffset] = useState(0);
  const [openFilter, setOpenFilter] = useState(null);

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
        return {
          fieldName: column.key,
          compare: normalizeFilterCompare(filter?.compare, column),
          value: coerceFilterValue(value, column),
        };
      })
      .filter(Boolean)),
    [columnFilters, fieldColumns],
  );

  const viewRows = useMemo(
    () => applyClientSorters(applyClientFilters(Array.isArray(rows) ? rows : [], filters, fieldColumns), sorters, fieldColumns),
    [rows, filters, sorters, fieldColumns],
  );

  // Keep the page offset in range as filtering shrinks the result set.
  useEffect(() => {
    if (offset > 0 && offset >= viewRows.length) setOffset(0);
  }, [viewRows.length, offset]);

  const pageRows = viewRows.slice(offset, offset + pageSize);

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
              <tr><td className="empty-cell" colSpan={columns.length}>{emptyText}</td></tr>
            )}
            {pageRows.map((row, idx) => (
              <tr key={row.id ?? idx}>
                {columns.map((column) => (
                  <td key={column.key}>{column.render ? column.render(row[column.key], row) : printable(row[column.key])}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager total={viewRows.length} offset={offset} limit={pageSize} onPage={setOffset} busy={busy} />
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
  const filterCount = normalizeFilterDrafts(filter, column).filter((item) => String(item.value ?? '').trim() !== '').length;
  const sortLabel = sort?.sort === 1 ? 'ASC' : sort?.sort === 2 ? 'DESC' : 'Sort';

  return (
    <div className="column-head">
      <div className="column-title-row">
        <span>{column.label}</span>
        <div className="column-actions">
          <button
            aria-label={`Filter ${column.label}`}
            className={filterCount > 0 ? 'filter-button active' : 'filter-button'}
            onClick={(event) => onFilterOpen(column, event.currentTarget)}
            title={`Filter ${column.label}`}
            type="button"
          >
            {filterCount > 1 && <span className="filter-count">{filterCount}</span>}
          </button>
          <button className={sort ? 'sort-button active' : 'sort-button'} onClick={() => onSort(column.key)} type="button" title={`Sort by ${column.label}`}>
            {sortLabel}
            {sort?.index && <span>{sort.index}</span>}
          </button>
        </div>
      </div>
    </div>
  );
}

function ColumnFilterPopover({ column, filter, left, top, onApply, onClear, onClose }) {
  const [draft, setDraft] = useState(() => normalizeFilterDrafts(filter, column));
  const operators = filterOperatorsForField(column);

  useEffect(() => { setDraft(normalizeFilterDrafts(filter, column)); }, [column, filter]);

  function updateDraft(index, patch) {
    setDraft((current) => current.map((item, i) => (i === index
      ? { ...item, ...patch, compare: normalizeFilterCompare(patch.compare ?? item.compare, column) }
      : item)));
  }

  return (
    <div className="filter-popover" style={{ left, top }}>
      <div className="filter-popover-head">
        <span>{column.label}</span>
        <button className="mini-button" onClick={onClose} type="button">Close</button>
      </div>
      <form className="filter-popover-body" onSubmit={(e) => { e.preventDefault(); onApply(column.key, draft); }}>
        {draft.map((item, index) => (
          <div className="filter-condition" key={`filter-${column.key}-${index}`}>
            <div className="filter-condition-head">
              <span>Condition {index + 1}</span>
              {draft.length > 1 && <button className="mini-button" onClick={() => setDraft((c) => c.filter((_, i) => i !== index))} type="button">Remove</button>}
            </div>
            <label>
              Operator
              <select value={normalizeFilterCompare(item.compare, column)} onChange={(e) => updateDraft(index, { compare: Number(e.target.value) })}>
                {operators.map((op) => <option key={op.value} value={op.value}>{op.label}</option>)}
              </select>
            </label>
            <label>
              Value
              {column.filterType === 'boolean' ? (
                <select value={item.value} onChange={(e) => updateDraft(index, { value: e.target.value })}>
                  <option value="">Any</option>
                  <option value="true">Yes</option>
                  <option value="false">No</option>
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
          <button className="secondary-button" onClick={() => setDraft((c) => [...c, createColumnFilter(column)])} type="button">Add</button>
          <button className="secondary-button" onClick={onClear} type="button">Clear</button>
          <button className="primary-button" type="submit">Apply</button>
        </div>
      </form>
    </div>
  );
}

function Pager({ total, offset, limit, onPage, busy }) {
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
      <span>Page {currentPage} / {pageCount} · {total} total</span>
      <div className="pager-controls">
        <button className="pager-icon first" disabled={busy || offset <= 0} onClick={() => onPage(0)} title="First page" type="button" aria-label="First page" />
        <button className="pager-icon previous" disabled={busy || offset <= 0} onClick={() => onPage(Math.max(0, offset - limit))} title="Previous page" type="button" aria-label="Previous page" />
        <label className="pager-jump">
          <input min="1" max={pageCount} value={pageDraft} onChange={(e) => setPageDraft(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); goToDraftPage(); } }} title="Page number" type="number" />
        </label>
        <button className="pager-icon go" disabled={busy} onClick={goToDraftPage} title="Go to page" type="button" aria-label="Go to page" />
        <button className="pager-icon next" disabled={busy || offset + limit >= total} onClick={() => onPage(offset + limit)} title="Next page" type="button" aria-label="Next page" />
        <button className="pager-icon last" disabled={busy || offset >= last} onClick={() => onPage(last)} title="Last page" type="button" aria-label="Last page" />
      </div>
    </div>
  );
}

// --- shared filter/sort helpers (ported verbatim from myidsan) ------------------

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
  if (field?.filterType === 'number') return FILTER_OPERATORS;
  return TEXT_FILTER_OPERATORS;
}

function normalizeFilterCompare(compare, field) {
  const value = Number(compare || 1);
  const operators = filterOperatorsForField(field);
  return operators.some((op) => op.value === value) ? value : operators[0].value;
}

function coerceFilterValue(value, field) {
  if (field?.filterType === 'boolean') return String(value).toLowerCase() === 'true';
  if (field?.filterType === 'number') return Number(value);
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
  if (field?.filterType === 'number') { const n = Number(value); return Number.isNaN(n) ? 0 : n; }
  if (field?.filterType === 'boolean') return value ? 1 : 0;
  return String(value ?? '').toLowerCase();
}

function compareFilterValue(actual, expected, compare, field) {
  if (field?.filterType === 'number') {
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
