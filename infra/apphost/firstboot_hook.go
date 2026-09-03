package apphost

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mysayasan/kopiv2/infra/apphost/firstboot"
	"github.com/mysayasan/kopiv2/infra/cache"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// runFirstBootSetup serves the pre-boot setup wizard when this install asks for one,
// and returns once config.json has been written. Startup then continues normally in
// this same process and loads the file the wizard produced — the whole point of doing
// it here rather than behind a restart.
//
// When no setup is needed (the overwhelmingly common case: every boot after the first)
// this costs one small file read and returns.
func runFirstBootSetup(app App, homeDir, dataDir string) error {
	// The wizard rewrites the data dir's config, so a packaged install must have its
	// shipped default seeded across first — otherwise there would be nothing to edit.
	if err := seedConfigIfMissing(homeDir, dataDir); err != nil {
		return err
	}
	configPath := configFilePath(dataDir)

	needed, reason, err := firstboot.Needed(configPath)
	if err != nil {
		return fmt.Errorf("setup check: %w", err)
	}
	if !needed {
		return nil
	}
	log.Printf("setup wizard: starting — %s", reason)

	// Signals are not handled until much later in boot, so the wizard installs its own
	// handler: an operator who abandons setup with Ctrl+C, or a service that is stopped
	// while the page is open, must exit cleanly rather than being killed mid-write.
	// Nothing is written until the final submit, which is a single atomic file replace.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result, err := firstboot.Run(ctx, firstBootOptions(app.Name(), configPath, dataDir))
	if err != nil {
		return fmt.Errorf("setup wizard: %w", err)
	}
	log.Printf("setup wizard: wrote %s; continuing startup", result.ConfigPath)
	return nil
}

// firstBootOptions assembles what the wizard needs from the host. It is a separate
// function so the wiring can be asserted: every capability here is optional to firstboot
// and silently degrades when nil — a missing Browser just means no browser opens, with
// no error anywhere — so "we forgot to pass one" is invisible at runtime and has to be
// caught by a test instead.
func firstBootOptions(appName, configPath, dataDir string) firstboot.Options {
	return firstboot.Options{
		AppName:    appName,
		ConfigPath: configPath,
		DataDir:    dataDir,
		Logf:       func(format string, args ...any) { log.Printf(format, args...) },
		Browser:    launchBrowser,
		ProbeDB:    probeDB(dataDir),
		ProbeCache: probeCache,
	}
}

// probeDB opens a real connection with the operator's answers and closes it again, so
// a wrong host, password or database name is reported while the form is still on
// screen. It goes through the same newDbCrud the boot itself uses — a probe that dialled
// the port some other way could pass on settings the app then fails to start with.
func probeDB(dataDir string) func(context.Context, firstboot.DBSettings) error {
	return func(ctx context.Context, settings firstboot.DBSettings) error {
		cfg := dbsql.DbConfigModel{
			Engine:   settings.Engine,
			Host:     settings.Host,
			Port:     settings.Port,
			User:     settings.User,
			Password: settings.Password,
			DbName:   settings.DBName,
			SslMode:  settings.SSLMode,
		}
		// A SQLite path is data-relative exactly as it is at boot, so the probe opens
		// the same file the app will.
		if normalizeDbEngine(cfg.Engine) == "sqlite" && cfg.DbName != ":memory:" {
			cfg.DbName = ResolveWritablePath(dataDir, cfg.DbName)
		}

		type dialResult struct {
			db  dbsql.IDbCrud
			err error
		}
		// newDbCrud pings during construction and offers no context, so the wait is
		// bounded here: an unreachable host would otherwise hang the setup page for
		// the driver's own connect timeout.
		done := make(chan dialResult, 1)
		go func() {
			db, err := newDbCrud(cfg)
			done <- dialResult{db: db, err: err}
		}()

		select {
		case <-ctx.Done():
			// The dial is left to finish and close itself; nothing else references it.
			go func() {
				if res := <-done; res.db != nil {
					closeQuietly(res.db)
				}
			}()
			return fmt.Errorf("connection timed out")
		case res := <-done:
			if res.err != nil {
				return res.err
			}
			defer closeQuietly(res.db)
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			return res.db.Ping(pingCtx)
		}
	}
}

// probeCache pings Redis with the operator's answers. It mirrors the in-app Settings
// editor's cache test, for the same reason: an unreachable cache is a boot failure, and
// finding that out here costs a click instead of a failed start.
func probeCache(ctx context.Context, settings firstboot.CacheSettings) error {
	store := cache.NewRedisStore(cache.RedisConfig{
		Address:          settings.Address,
		Password:         settings.Password,
		DB:               settings.DB,
		UseTLS:           settings.UseTLS,
		ConnectTimeout:   2 * time.Second,
		OperationTimeout: 2 * time.Second,
	})
	defer store.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return store.Ping(pingCtx)
}

// closeQuietly releases a probe's connection pool. Every engine's crud implements
// Close; the assertion keeps this honest if one ever stops.
func closeQuietly(db dbsql.IDbCrud) {
	if closer, ok := db.(io.Closer); ok {
		_ = closer.Close()
	}
}
