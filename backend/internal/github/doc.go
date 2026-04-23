// Package github implements just enough of the GitHub webhook protocol for
// Teal to receive push events securely.
//
// What it does:
//   - Validates the X-Hub-Signature-256 header in constant time.
//   - Parses the push-event JSON body to extract ref + head_commit.id.
//
// What it does NOT do:
//   - GitHub App auth (JWT → installation token). Defer to a later phase if
//     and when we ship the App-installation flow.
//   - Status checks, reviews, releases, or any non-push event types.
//   - HTTP. Validation and parsing are pure functions over headers + bytes;
//     the API layer wires them to a chi route.
package github
