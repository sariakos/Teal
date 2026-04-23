package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sariakos/teal/backend/internal/docker"
	"github.com/sariakos/teal/backend/internal/domain"
	"github.com/sariakos/teal/backend/internal/store"
)

// containerResponse / networkResponse / volumeResponse are the wire shapes
// for Docker resources. Translation lives in this file, mirroring the
// pattern used for App/User/Deployment.

type containerResponse struct {
	ID      string            `json:"id"`
	Names   []string          `json:"names"`
	Image   string            `json:"image"`
	State   string            `json:"state"`
	Status  string            `json:"status"`
	Created time.Time         `json:"created"`
	Labels  map[string]string `json:"labels"`
}

type networkResponse struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Driver string            `json:"driver"`
	Scope  string            `json:"scope"`
	Labels map[string]string `json:"labels"`
}

type volumeResponse struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	CreatedAt  time.Time         `json:"createdAt"`
	Labels     map[string]string `json:"labels"`
}

// dockerHandler exposes listings of the host's Docker objects plus the
// few mutations Teal needs (volume delete in v1).
type dockerHandler struct {
	logger *slog.Logger
	store  *store.Store
	docker docker.Client
}

func (h *dockerHandler) listContainers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.docker.ListContainers(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "docker: "+err.Error())
		return
	}
	out := make([]containerResponse, 0, len(rows))
	for _, c := range rows {
		out = append(out, containerResponse{
			ID: c.ID, Names: c.Names, Image: c.Image, State: c.State,
			Status: c.Status, Created: c.Created, Labels: c.Labels,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *dockerHandler) listNetworks(w http.ResponseWriter, r *http.Request) {
	rows, err := h.docker.ListNetworks(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "docker: "+err.Error())
		return
	}
	out := make([]networkResponse, 0, len(rows))
	for _, n := range rows {
		out = append(out, networkResponse{
			ID: n.ID, Name: n.Name, Driver: n.Driver, Scope: n.Scope, Labels: n.Labels,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *dockerHandler) listVolumes(w http.ResponseWriter, r *http.Request) {
	rows, err := h.docker.ListVolumes(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "docker: "+err.Error())
		return
	}
	// Optional ?app=<slug> filter restricts to volumes whose name starts
	// with one of the app's compose project prefixes (<slug>-blue_ or
	// <slug>-green_). Compose names auto-named volumes that way.
	appSlug := strings.TrimSpace(r.URL.Query().Get("app"))
	prefixes := []string{}
	if appSlug != "" {
		prefixes = []string{appSlug + "-blue_", appSlug + "-green_"}
	}
	out := make([]volumeResponse, 0, len(rows))
	for _, v := range rows {
		if len(prefixes) > 0 && !hasAnyPrefix(v.Name, prefixes) {
			continue
		}
		out = append(out, volumeResponse{
			Name: v.Name, Driver: v.Driver, Mountpoint: v.Mountpoint,
			CreatedAt: v.CreatedAt, Labels: v.Labels,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// deleteVolume removes a named docker volume after typed-name confirmation.
// Required: ?confirm=<name> (literal repeat) so a misclick can't blow away
// a database volume.
func (h *dockerHandler) deleteVolume(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing volume name")
		return
	}
	if r.URL.Query().Get("confirm") != name {
		writeError(w, http.StatusBadRequest, "confirm query parameter must equal the volume name")
		return
	}
	if err := h.docker.VolumeRemove(r.Context(), name, false); err != nil {
		if errors.Is(err, docker.ErrVolumeInUse) {
			writeError(w, http.StatusConflict, "volume is in use by a running container")
			return
		}
		writeError(w, http.StatusBadGateway, "docker: "+err.Error())
		return
	}
	recordAudit(r.Context(), h.logger, h.store.AuditLogs,
		domain.AuditActionVolumeDelete, "volume", name,
		clientIP(r), "deleted volume "+name, "")
	w.WriteHeader(http.StatusNoContent)
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
