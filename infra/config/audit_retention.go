package config

import "strings"

// Retention floor and defaults. The floor is the important one: a security trail trimmed to
// a week answers almost no investigative question, and "set retention to 1 day" is as
// plausibly an attacker covering their tracks as it is an operator saving disk. Anything
// below the floor is raised to it and warned about rather than honoured.
const (
	minAuditRetentionDays     = 30
	defaultAuditRetentionDays = 365
	defaultAuditPurgeHours    = 24
	defaultAuditArchiveDir    = "audit-archive"
)

// AuditRetentionConfigModel bounds how long the append-only security trail is kept in the
// database. It is OFF unless an operator turns it on: unbounded growth is a disk problem,
// but silent deletion of security history is a compliance and forensics problem, and only
// one of those is recoverable.
//
// There is deliberately no API for this. Retention is a config-file setting, so trimming
// the trail requires filesystem access to the server rather than a session on it — the
// superadmin whose own actions the trail records cannot reach for it from inside the
// product. And even with it on, an operator chooses only an age, never which rows go.
//
// Nothing is destroyed. Rows are written to a JSON-lines archive file BEFORE they leave the
// table, and a purge that cannot finish its archive deletes nothing. The purge itself is
// then recorded in the trail, naming the cutoff, the row count and the archive file, so a
// reader who finds the history starting abruptly can tell "this was trimmed on purpose, and
// here is where the rest lives" apart from "nothing happened before this date".
type AuditRetentionConfigModel struct {
	Retention struct {
		// Enabled defaults to false. Absent means keep everything.
		Enabled bool `json:"enabled"`
		// MaxRetentionDays is the age past which rows are archived out of the table.
		// Values below minAuditRetentionDays are raised to it.
		MaxRetentionDays int `json:"maxRetentionDays"`
		// FrequencyHours is how often the purge runs. Daily by default; there is no
		// value in checking a year-long retention window every few minutes.
		FrequencyHours int `json:"frequencyHours"`
		// ArchiveDir is where the JSON-lines archives are written. Relative paths
		// resolve against the data directory. The directory holds emails, source IPs
		// and user agents in the clear and must be protected accordingly — it is not
		// sealed, because an archive whose only key lives on the machine it was meant
		// to outlive is not an archive.
		ArchiveDir string `json:"archiveDir"`
	} `json:"retention"`
}

// EffectiveAuditRetention resolves the configured values against the defaults and the
// floor. clamped reports whether MaxRetentionDays was raised, so the caller can warn: an
// operator who asked for 7 days and silently got 30 would otherwise be surprised by a disk
// that keeps filling.
func (a AuditRetentionConfigModel) EffectiveAuditRetention() (cfg AuditRetentionEffective, clamped bool) {
	r := a.Retention
	cfg.Enabled = r.Enabled

	cfg.MaxRetentionDays = r.MaxRetentionDays
	if cfg.MaxRetentionDays <= 0 {
		cfg.MaxRetentionDays = defaultAuditRetentionDays
	} else if cfg.MaxRetentionDays < minAuditRetentionDays {
		cfg.MaxRetentionDays = minAuditRetentionDays
		clamped = true
	}

	cfg.FrequencyHours = r.FrequencyHours
	if cfg.FrequencyHours <= 0 {
		cfg.FrequencyHours = defaultAuditPurgeHours
	}

	cfg.ArchiveDir = strings.TrimSpace(r.ArchiveDir)
	if cfg.ArchiveDir == "" {
		cfg.ArchiveDir = defaultAuditArchiveDir
	}
	return cfg, clamped
}

// AuditRetentionEffective is the resolved form, with every value guaranteed usable.
type AuditRetentionEffective struct {
	Enabled          bool
	MaxRetentionDays int
	FrequencyHours   int
	ArchiveDir       string
}

// MinAuditRetentionDays exposes the floor for messages and tests.
func MinAuditRetentionDays() int { return minAuditRetentionDays }
