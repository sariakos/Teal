package githubapp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// InstallStateTTL is how long a state token remains valid. Long enough
// for the user to walk through GitHub's "pick which repos" UI; short
// enough that a leaked token can't be replayed days later.
const InstallStateTTL = 10 * time.Minute

// StateClaims is the payload Teal embeds in the install-flow state
// parameter. GitHub round-trips state opaquely back to the callback
// URL; we sign it ourselves with the platform secret to detect
// tampering and bind the redirect to the originating session.
type StateClaims struct {
	Slug    string `json:"slug"`
	UserID  int64  `json:"uid"`
	Nonce   string `json:"n"`
	Expires int64  `json:"e"` // unix seconds
}

// SignState produces a base64url(JSON|HMAC) state string. The caller
// embeds it in the GitHub install URL; on callback, ParseState verifies
// the HMAC + expiry before trusting the slug.
func SignState(secret []byte, slug string, userID int64) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("githubapp: state signing secret is empty")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	c := StateClaims{
		Slug:    slug,
		UserID:  userID,
		Nonce:   base64.RawURLEncoding.EncodeToString(nonce[:]),
		Expires: time.Now().UTC().Add(InstallStateTTL).Unix(),
	}
	body, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := mac.Sum(nil)
	// Wire format: base64url(body) + "." + base64url(sig)
	return base64.RawURLEncoding.EncodeToString(body) + "." +
		base64.RawURLEncoding.EncodeToString(sig), nil
}

// ParseState validates and unpacks a state string. Returns an error
// when the format is wrong, the HMAC fails, or the state has expired.
func ParseState(secret []byte, raw string) (StateClaims, error) {
	dot := -1
	for i := 0; i < len(raw); i++ {
		if raw[i] == '.' {
			dot = i
			break
		}
	}
	if dot < 1 || dot == len(raw)-1 {
		return StateClaims{}, errors.New("githubapp: malformed state")
	}
	bodyEnc, sigEnc := raw[:dot], raw[dot+1:]
	body, err := base64.RawURLEncoding.DecodeString(bodyEnc)
	if err != nil {
		return StateClaims{}, fmt.Errorf("githubapp: state body: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigEnc)
	if err != nil {
		return StateClaims{}, fmt.Errorf("githubapp: state sig: %w", err)
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	want := mac.Sum(nil)
	if !hmac.Equal(want, sig) {
		return StateClaims{}, errors.New("githubapp: state signature mismatch")
	}
	var c StateClaims
	if err := json.Unmarshal(body, &c); err != nil {
		return StateClaims{}, fmt.Errorf("githubapp: state json: %w", err)
	}
	if time.Now().UTC().Unix() > c.Expires {
		return StateClaims{}, fmt.Errorf("githubapp: state expired (was valid until %s)",
			time.Unix(c.Expires, 0).UTC().Format(time.RFC3339))
	}
	return c, nil
}

// InstallURL builds the github.com URL the user is redirected to so
// they can install the App on a chosen repo. App slug must match what
// the operator configured in platform_settings.
func InstallURL(appSlug, state string) string {
	if appSlug == "" {
		return ""
	}
	return "https://github.com/apps/" + appSlug + "/installations/new?state=" + state
}

// FetchInstallationRepo asks GitHub which repos an installation is
// scoped to and returns the first one's full_name. GitHub Apps with
// "All repositories" access return all the user's repos; for Teal we
// expect the user to pick one, so we just take the first to populate
// app.GitHubAppRepo. Caller should round-trip with the user if zero
// or many repos come back.
func FetchInstallationRepo(httpDoer HTTPDoer, cfg Config, installationID int64, now time.Time) (string, error) {
	jwt, err := MintAppJWT(cfg.AppID, cfg.PrivateKeyPEM, now)
	if err != nil {
		return "", err
	}
	url := "https://api.github.com/app/installations/" + strconv.FormatInt(installationID, 10) + "/access_tokens"
	req, err := http2NewRequest(nil, "POST", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err := httpDoer.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("githubapp: token request returned %d", resp.StatusCode)
	}
	var out struct {
		Repositories []struct {
			FullName string `json:"full_name"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Repositories) == 0 {
		return "", nil
	}
	return out.Repositories[0].FullName, nil
}
