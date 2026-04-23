package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// envHydrationResult is the output of hydrateEnv: the rendered env-file
// body, a SHA-256 hash over the canonical KEY=VALUE\n form, and any
// per-key warnings (e.g. an opted-in shared key that no longer exists).
type envHydrationResult struct {
	Body     []byte
	Hash     string   // hex SHA-256 of the canonical body; "" when set is empty
	Warnings []string // appended to the deploy log; never fatal
}

// hydrateEnv resolves all env vars an App should receive and writes them
// into a deterministic env-file body.
//
// Resolution order:
//  1. Per-app vars from env_vars (scope='app'). Decrypted with codec
//     purpose "envvar.app", AAD "app:<id>:envvar:<KEY>".
//  2. Shared vars the App has opted into via app_shared_env_vars.
//     Decrypted with purpose "envvar.shared", AAD "shared:envvar:<KEY>".
//
// On a key collision, per-app wins (last write wins after sorting; we
// implement it by populating the per-app set first and only adding shared
// keys that aren't already present).
//
// The returned hash is computed over the SAME bytes that go into the file,
// so two deploys with identical env produce identical hashes regardless
// of insertion order.
func hydrateEnv(ctx context.Context, st *store.Store, codec envCodec, app domain.App) (envHydrationResult, error) {
	pairs := map[string]string{} // key → plaintext value

	appVars, err := st.EnvVars.ListForApp(ctx, app.ID)
	if err != nil {
		return envHydrationResult{}, fmt.Errorf("list app env: %w", err)
	}
	for _, v := range appVars {
		plain, err := codec.Open(domain.CodecPurposeEnvVarApp, domain.EnvVarAppAAD(app.ID, v.Key), v.ValueEncrypted)
		if err != nil {
			return envHydrationResult{}, fmt.Errorf("decrypt app env %q: %w", v.Key, err)
		}
		pairs[v.Key] = string(plain)
	}

	sharedKeys, err := st.AppSharedEnvVars.ListForApp(ctx, app.ID)
	if err != nil {
		return envHydrationResult{}, fmt.Errorf("list shared allow-list: %w", err)
	}
	var warnings []string
	for _, key := range sharedKeys {
		shared, err := st.EnvVars.GetShared(ctx, key)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				warnings = append(warnings, fmt.Sprintf("shared env %q opted in but no shared row exists; skipped", key))
				continue
			}
			return envHydrationResult{}, fmt.Errorf("read shared env %q: %w", key, err)
		}
		if _, override := pairs[key]; override {
			warnings = append(warnings, fmt.Sprintf("shared env %q shadowed by per-app value", key))
			continue
		}
		plain, err := codec.Open(domain.CodecPurposeEnvVarShared, domain.EnvVarSharedAAD(key), shared.ValueEncrypted)
		if err != nil {
			return envHydrationResult{}, fmt.Errorf("decrypt shared env %q: %w", key, err)
		}
		pairs[key] = string(plain)
	}

	if len(pairs) == 0 {
		return envHydrationResult{Warnings: warnings}, nil
	}

	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(escapeEnvValue(pairs[k]))
		sb.WriteByte('\n')
	}
	body := []byte(sb.String())
	sum := sha256.Sum256(body)
	return envHydrationResult{
		Body:     body,
		Hash:     hex.EncodeToString(sum[:]),
		Warnings: warnings,
	}, nil
}

// envCodec is the subset of *crypto.Codec hydrateEnv needs. Defined here
// so tests can inject a fake without dragging in the real key derivation.
type envCodec interface {
	Open(purpose, aad string, ciphertext []byte) ([]byte, error)
}

// escapeEnvValue produces a docker-compose --env-file compatible value.
// Compose's env-file parser treats the entire RHS literally — no quoting
// is applied/required for values without newlines or carriage returns.
// We reject CR/LF in values during write at the API layer; here we just
// pass through.
func escapeEnvValue(v string) string {
	return v
}
