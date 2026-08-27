package dbsql

// scan_limit.go answers one question the three drivers used to answer wrongly, in triplicate:
// how many rows may a SELECT materialise?

// DefaultScanRowLimit bounds a query that asked for NO limit.
//
// A caller passing limit = 0 gets no LIMIT clause in the generated SQL, so without a ceiling here
// a single call could materialise the largest table in the schema — and the hot table in an
// appliance install (device_reading, recording_segment) grows forever. This is the historical
// value, kept deliberately: every caller that means to read more than this says so.
const DefaultScanRowLimit = uint64(100)

// ScanRowLimit is how many rows a driver's scan loop may take from a result set.
//
// WHAT THIS FIXES, because the shape of the bug is worth remembering. All three drivers used to
// hardcode a ceiling of 100 rows in the scan loop and apply it to EVERY query, whatever the
// caller asked for. The SQL still said LIMIT 2000; the database still found and returned the
// rows; the scan loop stopped at 100 and threw the rest away. Worse, the `x_rows_cnt` column is
// computed by the database over the whole CTE, so the total count came back TRUE — a caller had
// a page of 100 rows and a total of 2000 and no way to tell it had been truncated rather than
// paged.
//
// What that cost, measured on a running appliance: a telemetry chart asking for 2000 samples drew
// 100; a device page asking for the newest 500 readings to fold into "the current value of every
// key" saw 100, so a busy key crowded the others off the page entirely; and the flow runtime,
// asking for its 500 enabled flows, compiled 100 — an install's hundred-and-first flow was
// listed nowhere and never ran, with no error at any layer. Seventy call sites across the suite
// ask for more than a hundred rows.
//
// The rule now: if the caller gave a limit, the SQL already bounds the result, so the scan takes
// exactly what was asked for. Only a caller that asked for everything gets a ceiling imposed on
// it.
func ScanRowLimit(limit uint64) uint64 {
	if limit > 0 {
		return limit
	}
	return DefaultScanRowLimit
}
