package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/githubapp"
	"github.com/sariakos/teal/backend/internal/store"
)

// gitHubAppReposHandler powers the "pick a repo" dropdown the per-app
// Settings tab uses when GitHub App auth is selected. Returns one
// entry per installation of the platform App, each carrying the list
// of repositories that installation can see.
//
// This replaces the old "click install → bounce to GitHub → wait for
// redirect → pick repo on Teal" flow with a single in-page select
// when the installation already exists. The redirect flow is kept as
// a fallback for the empty case (no installations yet).
type gitHubAppReposHandler struct {
	logger     *slog.Logger
	store      *store.Store
	codec      *crypto.Codec
	tokenCache *githubapp.TokenCache
	httpDoer   githubapp.HTTPDoer // nil → http.DefaultClient
}

type repoEntry struct {
	FullName      string `json:"fullName"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"defaultBranch"`
}

type installationEntry struct {
	InstallationID int64       `json:"installationId"`
	AccountLogin   string      `json:"accountLogin"`
	AccountType    string      `json:"accountType"` // "User" / "Organization"
	Repos          []repoEntry `json:"repos"`
}

type reposResponse struct {
	// Configured is false when the platform App hasn't been set up
	// yet — UI shows a "configure the platform App first" hint that
	// links to /settings/github-app.
	Configured bool `json:"configured"`

	// AppSlug lets the UI render the "Install on more repos" link
	// (https://github.com/apps/<slug>/installations/new) when the
	// user wants to add an installation.
	AppSlug string `json:"appSlug,omitempty"`

	// Installations is one entry per installation. Empty when the App
	// is configured but installed nowhere — UI prompts the user to
	// click the install link.
	Installations []installationEntry `json:"installations"`
}

// list handles GET /apps/{slug}/github-app/repos. The slug is only
// used to 404 a bogus app — the response is the same global view of
// installations a future per-app filter would narrow.
func (h *gitHubAppReposHandler) list(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, err := h.store.Apps.GetBySlug(r.Context(), slug); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "app not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "lookup app")
		return
	}
	out, err := h.fetch(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// listGlobal handles GET /github-app/repos. Same payload, no slug
// constraint — used by the new-app form (no app exists yet).
func (h *gitHubAppReposHandler) listGlobal(w http.ResponseWriter, r *http.Request) {
	out, err := h.fetch(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// fetch is the shared data-loading body. Returns reposResponse with
// Configured=false when the platform App isn't set up yet (callers
// 200 that — it's a useful state for the UI, not an error).
func (h *gitHubAppReposHandler) fetch(r *http.Request) (reposResponse, error) {
	cfg, err := githubapp.LoadConfig(r.Context(), h.store, h.codec)
	if err != nil {
		return reposResponse{}, fmt.Errorf("load app config: %w", err)
	}
	if !cfg.Configured() {
		return reposResponse{Configured: false}, nil
	}
	insts, err := githubapp.ListInstallations(r.Context(), h.httpDoer, cfg, time.Now())
	if err != nil {
		return reposResponse{}, fmt.Errorf("list installations: %w", err)
	}
	out := reposResponse{Configured: true, AppSlug: cfg.AppSlug}
	for _, ins := range insts {
		entry := installationEntry{
			InstallationID: ins.ID,
			AccountLogin:   ins.AccountLogin,
			AccountType:    ins.Account.Type,
		}
		// Mint (or reuse) an installation token via the shared cache,
		// then list repos. A failure here is per-installation, not
		// fatal — surface the rest so the user can still pick from
		// installations that worked.
		token, terr := h.tokenCache.Get(r.Context(), cfg, ins.ID)
		if terr != nil {
			h.logger.Warn("installation token: skipping", "installation", ins.ID, "err", terr)
			out.Installations = append(out.Installations, entry)
			continue
		}
		repos, rerr := githubapp.ListInstallationRepos(r.Context(), h.httpDoer, token)
		if rerr != nil {
			h.logger.Warn("list installation repos: skipping", "installation", ins.ID, "err", rerr)
			out.Installations = append(out.Installations, entry)
			continue
		}
		entry.Repos = make([]repoEntry, 0, len(repos))
		for _, rp := range repos {
			entry.Repos = append(entry.Repos, repoEntry{
				FullName:      rp.FullName,
				Private:       rp.Private,
				DefaultBranch: rp.DefaultBranch,
			})
		}
		out.Installations = append(out.Installations, entry)
	}
	return out, nil
}
