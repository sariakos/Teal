package githubapp

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// Codec purposes used to wrap the App's secrets at rest.
const (
	CodecPurposePrivateKey    = "github_app.private_key"
	CodecPurposeWebhookSecret = "github_app.webhook_secret"
)

// Platform-settings keys carrying GitHub App config. Plaintext keys
// (numeric ID, slug) live in TEXT columns; BLOB-shaped secrets are
// stored encrypted under a dedicated codec purpose with a static AAD.
const (
	SettingAppID                  = "github_app.app_id"
	SettingAppSlug                = "github_app.app_slug"
	SettingPrivateKeyEncryptedB64 = "github_app.private_key_b64"
	SettingWebhookSecretEncryptedB64 = "github_app.webhook_secret_b64"
)

// AAD value for both private-key and webhook-secret seal/open. Static
// — the App is single-instance per platform, so binding to a specific
// App ID would just mean re-encrypting on every rotation without
// adding security. Distinct codec purposes already ensure cross-use
// can't alias.
const aad = "platform"

// LoadConfig reads and decrypts the GitHub App config from
// platform_settings. Returns a Config with Configured()==false (and
// nil error) when nothing has been saved yet — callers handle the
// "App not set up" case by checking Configured.
func LoadConfig(ctx context.Context, st *store.Store, codec *crypto.Codec) (Config, error) {
	if st == nil || codec == nil {
		return Config{}, errors.New("githubapp: store and codec required")
	}
	idStr, err := st.PlatformSettings.GetOrDefault(ctx, SettingAppID, "")
	if err != nil {
		return Config{}, err
	}
	if idStr == "" {
		return Config{}, nil
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return Config{}, fmt.Errorf("githubapp: %s is not numeric: %w", SettingAppID, err)
	}
	slug, _ := st.PlatformSettings.GetOrDefault(ctx, SettingAppSlug, "")

	privEnc, _ := st.PlatformSettings.GetOrDefault(ctx, SettingPrivateKeyEncryptedB64, "")
	whEnc, _ := st.PlatformSettings.GetOrDefault(ctx, SettingWebhookSecretEncryptedB64, "")

	cfg := Config{AppID: id, AppSlug: slug}
	if privEnc != "" {
		raw, err := decodeAndOpen(codec, CodecPurposePrivateKey, privEnc)
		if err != nil {
			return Config{}, fmt.Errorf("githubapp: decrypt private key: %w", err)
		}
		cfg.PrivateKeyPEM = raw
	}
	if whEnc != "" {
		raw, err := decodeAndOpen(codec, CodecPurposeWebhookSecret, whEnc)
		if err != nil {
			return Config{}, fmt.Errorf("githubapp: decrypt webhook secret: %w", err)
		}
		cfg.WebhookSecret = raw
	}
	return cfg, nil
}

// SaveSecret encrypts secret under the given codec purpose and writes
// it to platform_settings as base64. Used by the admin settings
// handler when the operator pastes a new private key or webhook
// secret. Returns the (already-stored) base64 string for symmetry
// with the read path.
func SaveSecret(ctx context.Context, st *store.Store, codec *crypto.Codec, purpose, settingKey string, secret []byte) error {
	enc, err := codec.Seal(purpose, aad, secret)
	if err != nil {
		return fmt.Errorf("githubapp: seal: %w", err)
	}
	return st.PlatformSettings.Set(ctx, settingKey, encodeBase64(enc))
}

// FindAppByRepo looks up which Teal app corresponds to a GitHub
// "owner/repo" full name. Used by the centralized webhook to route
// push events. Returns ErrNotFound when no Teal app is installed on
// that repo.
func FindAppByRepo(ctx context.Context, st *store.Store, fullName string) (domain.App, error) {
	if fullName == "" {
		return domain.App{}, store.ErrNotFound
	}
	apps, err := st.Apps.List(ctx)
	if err != nil {
		return domain.App{}, err
	}
	for _, a := range apps {
		if a.GitHubAppRepo == fullName {
			return a, nil
		}
	}
	return domain.App{}, store.ErrNotFound
}
