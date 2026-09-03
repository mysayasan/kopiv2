// Package firstboot serves the pre-boot setup wizard.
//
// WHY THIS EXISTS. Every infrastructure block an app needs to come up at all — the
// database, the cache, the listen ports, the bootstrap administrator — is read by
// infra/apphost exactly once, at boot, before the app is ever handed a database handle.
// That ordering has a hard consequence: an install whose db or cache settings are wrong
// cannot be fixed from inside the app, because the app never gets far enough to serve a
// page. The in-app Settings screen only works once the app is already up, and all it can
// do afterwards is ask for a restart.
//
// So the wizard runs BEFORE any of that, in the same process, on its own port: it needs
// no database, no cache, no session, and no app wiring. It writes config.json, and then
// apphost carries on with the normal boot sequence and reads the file the wizard just
// wrote. There is no restart, no supervisor dependency, and no second process to keep in
// step — the operator's browser is handed the real app's URL as it comes up.
//
// WHEN IT RUNS. Never by accident. A transient database outage must not flip a running
// fleet control plane into a configuration wizard, so reachability is deliberately NOT a
// trigger. It runs only when the config file says so explicitly ("setup": {"completed":
// false}, which is what a fresh packaged install ships) or when an operator asks for it
// with KOPIV2_SETUP=1 — the recovery path for an install that can no longer boot. An
// existing config.json with no "setup" block is treated as already set up, so no
// deployed install ever meets a wizard it did not ask for.
package firstboot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mysayasan/kopiv2/infra/config/configfile"
)

// SetupBlockKey is the top-level config.json key holding the wizard's own state.
// Only "completed" is required; the rest tune where the wizard listens.
//
//	"setup": { "completed": false, "address": "127.0.0.1:39530", "allowRemote": false }
const SetupBlockKey = "setup"

// setupBlock is the wizard's own config block. Completed is a POINTER on purpose:
// absent (nil) means "this install predates the wizard, leave it alone", which is a
// different answer from an explicit false ("a fresh install, ask the operator").
type setupBlock struct {
	Completed   *bool  `json:"completed"`
	Address     string `json:"address"`
	AllowRemote bool   `json:"allowRemote"`
}

// DBSettings is the database step's answer. It mirrors dbsql.DbConfigModel's JSON shape
// in config.json — note that db_name and ssl_mode are snake_case there.
type DBSettings struct {
	Engine   string `json:"engine"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"db_name"`
	SSLMode  string `json:"ssl_mode"`
}

// CacheSettings is the cache step's answer. Provider "default" is the in-process store,
// which needs nothing else configured.
type CacheSettings struct {
	Provider string `json:"provider"`
	Address  string `json:"address"`
	Password string `json:"password"`
	DB       int    `json:"db"`
	UseTLS   bool   `json:"useTls"`
}

// WebSettings is the listen-address step's answer.
type WebSettings struct {
	EnableTLS   bool     `json:"enableTls"`
	TLSPorts    []int    `json:"tlsPorts"`
	NonTLSPorts []int    `json:"nonTlsPorts"`
	Hostnames   []string `json:"hostnames"`
}

// AdminSettings is the bootstrap administrator step's answer. A blank password keeps
// whatever the config already holds — including none at all, in which case the app
// generates one on first run and announces it.
type AdminSettings struct {
	Enabled  bool   `json:"enabled"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Answers is the whole wizard, as submitted.
type Answers struct {
	DB    DBSettings    `json:"db"`
	Cache CacheSettings `json:"cache"`
	Web   WebSettings   `json:"web"`
	Admin AdminSettings `json:"admin"`
}

// Options configure a wizard run.
type Options struct {
	// AppName labels the page and the console banner.
	AppName string
	// ConfigPath is the config.json the wizard reads and rewrites. It must already
	// exist — every install ships one.
	ConfigPath string
	// DataDir receives SETUP_URL.txt, so an operator running the app as a Windows
	// service (no visible console) can still find the wizard.
	DataDir string
	// Logf receives progress lines. Optional.
	Logf func(format string, args ...any)
	// Browser opens the setup URL for the operator and reports what happened: whether
	// a browser was launched and, if not, why. The banner repeats the reason, because
	// "I ran it and nothing appeared" is otherwise indistinguishable from a crash.
	// Optional — a nil Browser simply prints the URL, which is what a headless install
	// wants anyway.
	Browser func(url string) (opened bool, reason string)
	// ProbeDB and ProbeCache attempt a real connection with the supplied settings, so
	// the operator finds out here rather than after a failed boot. Optional: a nil
	// probe reports "not verified" rather than pretending it passed.
	ProbeDB    func(ctx context.Context, cfg DBSettings) error
	ProbeCache func(ctx context.Context, cfg CacheSettings) error
}

// Result reports what a completed wizard did.
type Result struct {
	// ConfigPath is the file that was written.
	ConfigPath string
	// StartURL is where the real app will be reachable, given the answers.
	StartURL string
}

// Needed reports whether the wizard should run for this config file, and why.
//
// The decision is deliberately narrow — see the package comment. Reachability of any
// dependency is never consulted.
func Needed(configPath string) (bool, string, error) {
	forced, explicit := envSetupOverride()
	if explicit {
		if forced {
			return true, "KOPIV2_SETUP requests the setup wizard", nil
		}
		return false, "KOPIV2_SETUP disables the setup wizard", nil
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		// A missing config file is not this package's problem to report: apphost's
		// own loader gives the operator a far better message about where it looked.
		if errors.Is(err, os.ErrNotExist) {
			return false, "no config file", nil
		}
		return false, "", fmt.Errorf("read config: %w", err)
	}
	block, err := readSetupBlock(raw)
	if err != nil {
		return false, "", err
	}
	if block.Completed == nil {
		// An install that predates the wizard, or one whose operator configured the
		// file by hand. Either way it is already set up; never ambush it.
		return false, "config has no setup block", nil
	}
	if *block.Completed {
		return false, "setup already completed", nil
	}
	return true, "config marks setup as not completed", nil
}

// envSetupOverride reads KOPIV2_SETUP. explicit reports that the variable held a value
// this understands, so an unrecognized value falls back to the config file rather than
// silently meaning "off".
func envSetupOverride() (force bool, explicit bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KOPIV2_SETUP"))) {
	case "1", "true", "yes", "on", "force":
		return true, true
	case "0", "false", "no", "off", "never":
		return false, true
	}
	return false, false
}

func readSetupBlock(raw []byte) (setupBlock, error) {
	var doc struct {
		Setup *setupBlock `json:"setup"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return setupBlock{}, fmt.Errorf("parse config: %w", err)
	}
	if doc.Setup == nil {
		return setupBlock{}, nil
	}
	return *doc.Setup, nil
}

// currentAnswers reads the config file into the wizard's shape, so every field opens
// pre-filled with what the install already ships rather than with a blank form.
func currentAnswers(raw []byte) (Answers, error) {
	var doc struct {
		DB    DBSettings `json:"db"`
		Cache struct {
			Provider string        `json:"provider"`
			Redis    CacheSettings `json:"redis"`
		} `json:"cache"`
		Server struct {
			Hostnames    []string `json:"hostnames"`
			TLSPorts     []int    `json:"tlsPorts"`
			NonTLSPorts  []int    `json:"nonTlsPorts"`
			Ports        []int    `json:"ports"`
			EnableTLS    *bool    `json:"enableTls"`
			EnableNonTLS *bool    `json:"enableNonTls"`
		} `json:"server"`
		LocalAuth AdminSettings `json:"localAuth"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Answers{}, fmt.Errorf("parse config: %w", err)
	}

	a := Answers{DB: doc.DB, Admin: doc.LocalAuth}
	a.Cache = doc.Cache.Redis
	a.Cache.Provider = doc.Cache.Provider
	a.Web = WebSettings{
		Hostnames:   doc.Server.Hostnames,
		TLSPorts:    doc.Server.TLSPorts,
		NonTLSPorts: doc.Server.NonTLSPorts,
	}
	// "ports" is the legacy single list, still honoured by the host. Show it under
	// whichever of the two lists the config's TLS switch says it is really serving,
	// or the operator would see an empty form for an install that listens fine.
	if len(a.Web.TLSPorts) == 0 && len(a.Web.NonTLSPorts) == 0 && len(doc.Server.Ports) > 0 {
		if doc.Server.EnableTLS == nil || *doc.Server.EnableTLS {
			a.Web.TLSPorts = doc.Server.Ports
		} else {
			a.Web.NonTLSPorts = doc.Server.Ports
		}
	}
	a.Web.EnableTLS = len(a.Web.TLSPorts) > 0
	if doc.Server.EnableTLS != nil {
		a.Web.EnableTLS = *doc.Server.EnableTLS && len(a.Web.TLSPorts) > 0
	}
	return a, nil
}

// commit writes the answers into config.json and marks setup complete. Every value is
// written as an explicit leaf patch, so blocks the wizard does not ask about — and the
// untouched keys inside the blocks it does — keep their bytes exactly as shipped.
func commit(configPath string, a Answers) error {
	patches := []configfile.Patch{
		{Path: []string{"db", "engine"}, Value: a.DB.Engine},
		{Path: []string{"db", "host"}, Value: a.DB.Host},
		{Path: []string{"db", "port"}, Value: a.DB.Port},
		{Path: []string{"db", "user"}, Value: a.DB.User},
		{Path: []string{"db", "password"}, Value: a.DB.Password},
		{Path: []string{"db", "db_name"}, Value: a.DB.DBName},
		{Path: []string{"db", "ssl_mode"}, Value: a.DB.SSLMode},

		{Path: []string{"cache", "provider"}, Value: a.Cache.Provider},

		{Path: []string{"server", "hostnames"}, Value: a.Web.Hostnames},
		{Path: []string{"server", "tlsPorts"}, Value: a.Web.TLSPorts},
		{Path: []string{"server", "nonTlsPorts"}, Value: a.Web.NonTLSPorts},
		{Path: []string{"server", "enableTls"}, Value: a.Web.EnableTLS},
		{Path: []string{"server", "enableNonTls"}, Value: len(a.Web.NonTLSPorts) > 0},

		{Path: []string{"localAuth", "enabled"}, Value: a.Admin.Enabled},
		{Path: []string{"localAuth", "username"}, Value: a.Admin.Username},
		{Path: []string{"localAuth", "password"}, Value: a.Admin.Password},

		{Path: []string{SetupBlockKey, "completed"}, Value: true},
	}
	// The Redis leaves are written only when Redis is actually chosen. Writing them
	// unconditionally would stamp a blank address over a working one every time an
	// operator picked the in-process cache.
	if isRedis(a.Cache.Provider) {
		patches = append(patches,
			configfile.Patch{Path: []string{"cache", "redis", "address"}, Value: a.Cache.Address},
			configfile.Patch{Path: []string{"cache", "redis", "password"}, Value: a.Cache.Password},
			configfile.Patch{Path: []string{"cache", "redis", "db"}, Value: a.Cache.DB},
			configfile.Patch{Path: []string{"cache", "redis", "useTls"}, Value: a.Cache.UseTLS},
		)
	}
	return configfile.Materialize(configPath, patches)
}

func isRedis(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "redis")
}

// isSQLite reports the one engine that needs no host, port or credentials — the
// difference that drives both the form and the validation.
func isSQLite(engine string) bool {
	return strings.EqualFold(strings.TrimSpace(engine), "sqlite")
}
