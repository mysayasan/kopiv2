package app

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/apis"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/atrest"
)

// atrestBoot is the outcome of resolving the encryption-at-rest master key.
//
// RecoveryPending is the one that matters: when encryption is on and the key is missing
// but a key existed here before, the app must NOT continue. It mounts only the public
// recovery gate and returns, so no service can write plaintext and no replacement key is
// minted, until the operator restores the key. Returning a flag rather than doing that
// inline keeps the decision visible in the composition root instead of buried in a
// hundred lines of key handling.
type atrestBoot struct {
	KeyStore *atrest.KeyStore
	Cipher   *atrest.Cipher
	// RecoveryPending is true when the app must start in RECOVERY mode and do nothing else.
	RecoveryPending bool
}

// openAtRest resolves the encryption-at-rest master key. It runs FIRST, before any service
// is built, precisely so that a missing key cannot race a service into writing plaintext.
//
// The key deliberately lives OUTSIDE the media roots, so a factory reset can destroy it
// explicitly (KeyStore.Destroy) — that is what makes the wipe instant and
// device-independent rather than a scrub of every file.
//
// When RecoveryPending is returned, the caller must mount nothing else and return.
func openAtRest(api *mux.Router, deps apphost.Dependencies) (atrestBoot, error) {
	if !boolValue(deps.Config.Security.EncryptAtRest, true) {
		return atrestBoot{}, nil
	}

	keyPath := strings.TrimSpace(deps.Config.Security.KeyPath)
	if keyPath == "" {
		// ResolveWritablePath keeps an existing legacy key (pre-packaging:
		// CWD/secret/atrest.key) in place so upgrades don't orphan encrypted footage.
		keyPath, _ = filepath.Abs(apphost.ResolveWritablePath(deps.DataDir, filepath.Join("secret", "atrest.key")))
	}
	// A disaster-recovery escrow placed here (or configured) is restored automatically on
	// boot when no key exists yet. Defaults to recovery.atrestkey beside the key.
	recoveryPath := strings.TrimSpace(deps.Config.Security.RecoveryPath)
	if recoveryPath == "" {
		recoveryPath = filepath.Join(filepath.Dir(keyPath), "recovery.atrestkey")
	}
	protectorCfg := atrest.ProtectorConfig{
		Name:           deps.Config.Security.KeyProtector,
		Passphrase:     deps.Config.Security.Passphrase,
		PassphraseFile: deps.Config.Security.PassphraseFile,
		PassphraseEnv:  deps.Config.Security.PassphraseEnv,
	}

	outcome, err := atrest.OpenForStartup(keyPath, recoveryPath, protectorCfg)
	if err != nil {
		return atrestBoot{}, fmt.Errorf("encryption-at-rest key: %w", err)
	}

	if outcome.Mode == atrest.ModeRecoveryPending {
		apis.NewRecoveryGateApi(api, keyPath, protectorCfg, deps.Restarter, outcome.KeyId)
		log.Printf("encryption-at-rest: master key missing but a key existed here before (id %s) — starting in RECOVERY mode; open the app to restore it", outcome.KeyId)
		return atrestBoot{RecoveryPending: true}, nil
	}

	protector := strings.TrimSpace(deps.Config.Security.KeyProtector)
	if protector == "" {
		protector = "file"
	}
	if outcome.Mode == atrest.ModeRestored {
		log.Printf("encryption-at-rest: restored master key from recovery file %s", recoveryPath)
	}
	log.Printf("encryption-at-rest enabled (key %s, protector %s, id %s)", keyPath, protector, outcome.KeyId)

	return atrestBoot{KeyStore: outcome.KeyStore, Cipher: outcome.KeyStore.Cipher()}, nil
}
