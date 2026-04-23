// Package git owns Teal's read-only git operations: shallow clones into
// per-deployment working dirs and per-app SSH deploy-key generation.
//
// What it does:
//   - Generates ed25519 keypairs for SSH deploy keys (PEM private,
//     OpenSSH public single-line, RFC 4253 fingerprint).
//   - Shells out to `git clone --depth 1 --branch X` with three auth modes:
//     public (URL as-is), PAT (URL rewritten with token), SSH (per-clone
//     temp identity file + GIT_SSH_COMMAND).
//   - Reports the resolved commit SHA via `git rev-parse HEAD`.
//
// What it does NOT do:
//   - Push, fetch, pull on existing checkouts. v1 always clones fresh.
//   - Cache. No bare-clone optimisation; reconsider if deploy times become
//     painful.
//   - Decrypt credentials. Callers (the deploy engine) decrypt via
//     internal/crypto and pass plaintext into Auth.
package git
