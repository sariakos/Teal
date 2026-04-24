package api

import (
	"strings"

	"github.com/sariakos/teal/backend/internal/domain"
)

// normalizeRoutes runs the same hostname normalisation joinDomains
// applies to the legacy Domains field across every route's Domain.
// Empty Domain entries get dropped — a route without a host is a
// configuration mistake, not something Traefik can render.
//
// Service names are trimmed but not lowercased — compose service
// names are case-sensitive and "App" vs "app" are different services.
func normalizeRoutes(in []domain.Route) []domain.Route {
	out := make([]domain.Route, 0, len(in))
	for _, r := range in {
		domain := normalizeDomain(r.Domain)
		if domain == "" {
			continue
		}
		out = append(out, normalizeRouteShape(r, domain))
	}
	return out
}

func normalizeRouteShape(r domain.Route, normalisedDomain string) domain.Route {
	r.Domain = normalisedDomain
	r.Service = strings.TrimSpace(r.Service)
	if r.Port < 0 {
		r.Port = 0
	}
	return r
}
