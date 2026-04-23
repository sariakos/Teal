package domain

import (
	"strconv"
	"time"
)

// EnvVar codec purposes. The crypto.Codec uses these strings to derive
// per-purpose keys; never reuse a purpose for unrelated data.
const (
	CodecPurposeEnvVarApp    = "envvar.app"
	CodecPurposeEnvVarShared = "envvar.shared"
)

// EnvVarAppAAD binds an app-scoped env-var ciphertext to (app, key) so a
// ciphertext sealed for one slot fails to decrypt when bound to another.
// Both the API (write path) and the engine (read path) MUST call this.
func EnvVarAppAAD(appID int64, key string) string {
	return "app:" + strconv.FormatInt(appID, 10) + ":envvar:" + key
}

// EnvVarSharedAAD binds a shared env-var ciphertext to its key.
func EnvVarSharedAAD(key string) string {
	return "shared:envvar:" + key
}

// EnvVarScope distinguishes per-app variables from shared secrets that can be
// referenced across multiple Apps (spec §4 — "shared env vars").
type EnvVarScope string

const (
	EnvVarScopeApp    EnvVarScope = "app"
	EnvVarScopeShared EnvVarScope = "shared"
)

// EnvVar is one key/value pair injected into a Deployment's environment.
// Values are encrypted at rest using a key derived from the platform secret;
// the ciphertext is what is stored in ValueEncrypted.
//
// Identity rules:
//   - For Scope == "app": (AppID, Key) is unique. AppID must be non-nil.
//   - For Scope == "shared": Key is unique platform-wide. AppID must be nil.
type EnvVar struct {
	ID    int64
	Scope EnvVarScope

	// AppID is set only for app-scoped variables. Use a pointer so the
	// repository can map a NULL column unambiguously.
	AppID *int64

	Key             string
	ValueEncrypted  []byte // AES-256-GCM ciphertext (nonce + ciphertext + tag)

	CreatedAt time.Time
	UpdatedAt time.Time
}
