package firstboot

import (
	"fmt"
	"strings"
)

// supportedEngines are the database engines infra/apphost can actually dial. Keeping
// the list here rather than accepting whatever the browser sent means a typo is caught
// while the operator is still looking at the form, not on the next boot.
var supportedEngines = []string{"postgres", "mariadb", "sqlite"}

// supportedCacheProviders mirrors the host's buildCacheStore switch. "default" is the
// in-process store.
var supportedCacheProviders = []string{"default", "redis"}

// validate checks the answers as a whole and returns a message aimed at the operator,
// naming the field. reservedPort is the wizard's own listening port: an app told to
// listen there would fail to bind on the boot that follows, which is precisely the
// unbootable state this wizard exists to prevent.
func validate(a *Answers, reservedPort int) error {
	if err := validateDB(&a.DB); err != nil {
		return err
	}
	if err := validateCache(&a.Cache); err != nil {
		return err
	}
	if err := validateWeb(&a.Web, reservedPort); err != nil {
		return err
	}
	return validateAdmin(&a.Admin)
}

func validateDB(db *DBSettings) error {
	db.Engine = strings.ToLower(strings.TrimSpace(db.Engine))
	if db.Engine == "" {
		db.Engine = "postgres"
	}
	if !contains(supportedEngines, db.Engine) {
		return fmt.Errorf("database engine must be one of %s", strings.Join(supportedEngines, ", "))
	}
	db.Host = strings.TrimSpace(db.Host)
	db.User = strings.TrimSpace(db.User)
	db.DBName = strings.TrimSpace(db.DBName)
	db.SSLMode = strings.TrimSpace(db.SSLMode)

	if isSQLite(db.Engine) {
		if db.DBName == "" {
			return fmt.Errorf("a database file path is required for SQLite")
		}
		// A SQLite install has no server to reach, so any host/port/credentials left
		// over from a previous engine are cleared rather than carried into the file,
		// where they would read as configuration that means something.
		db.Host, db.User, db.Password, db.SSLMode = "", "", "", ""
		db.Port = 0
		return nil
	}
	if db.Host == "" {
		return fmt.Errorf("a database host is required")
	}
	if db.Port <= 0 || db.Port > 65535 {
		return fmt.Errorf("the database port must be between 1 and 65535")
	}
	if db.User == "" {
		return fmt.Errorf("a database user is required")
	}
	if db.DBName == "" {
		return fmt.Errorf("a database name is required")
	}
	return nil
}

func validateCache(c *CacheSettings) error {
	c.Provider = strings.ToLower(strings.TrimSpace(c.Provider))
	if c.Provider == "" {
		c.Provider = "default"
	}
	if !contains(supportedCacheProviders, c.Provider) {
		return fmt.Errorf("cache provider must be one of %s", strings.Join(supportedCacheProviders, ", "))
	}
	if !isRedis(c.Provider) {
		return nil
	}
	c.Address = strings.TrimSpace(c.Address)
	if c.Address == "" {
		return fmt.Errorf("a Redis address is required")
	}
	if c.DB < 0 {
		return fmt.Errorf("the Redis database number cannot be negative")
	}
	return nil
}

func validateWeb(w *WebSettings, reservedPort int) error {
	w.TLSPorts = normalizePorts(w.TLSPorts)
	w.NonTLSPorts = normalizePorts(w.NonTLSPorts)
	if len(w.Hostnames) == 0 {
		w.Hostnames = []string{"*"}
	}
	if len(w.TLSPorts) == 0 && len(w.NonTLSPorts) == 0 {
		return fmt.Errorf("at least one HTTP or HTTPS port is required")
	}
	if w.EnableTLS && len(w.TLSPorts) == 0 {
		return fmt.Errorf("HTTPS is switched on but no HTTPS port is set")
	}
	if !w.EnableTLS && len(w.TLSPorts) > 0 {
		// The host reads enableTls to decide whether to serve the TLS list at all.
		// Rather than silently dropping the ports the operator just typed, take the
		// ports as the intent.
		w.EnableTLS = true
	}
	seen := map[int]bool{}
	for _, port := range append(append([]int{}, w.TLSPorts...), w.NonTLSPorts...) {
		if port <= 0 || port > 65535 {
			return fmt.Errorf("port %d is outside 1-65535", port)
		}
		if seen[port] {
			return fmt.Errorf("port %d is listed twice", port)
		}
		seen[port] = true
		if reservedPort > 0 && port == reservedPort {
			return fmt.Errorf("port %d is the one this setup page is using; pick another for the app", port)
		}
	}
	return nil
}

func validateAdmin(a *AdminSettings) error {
	a.Username = strings.TrimSpace(a.Username)
	if !a.Enabled {
		return nil
	}
	if a.Username == "" {
		return fmt.Errorf("an administrator username is required")
	}
	// The password may be blank: that means "keep what the config already has", and
	// when the config has none the app generates one on first run and announces it.
	if pw := a.Password; pw != "" && len(pw) < 8 {
		return fmt.Errorf("the administrator password must be at least 8 characters")
	}
	return nil
}

// normalizePorts drops zero/blank entries the form sends for empty rows, preserving order.
func normalizePorts(ports []int) []int {
	out := make([]int, 0, len(ports))
	for _, p := range ports {
		if p != 0 {
			out = append(out, p)
		}
	}
	return out
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
