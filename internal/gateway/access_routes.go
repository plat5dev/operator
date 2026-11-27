package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/plat5dev/operator/internal/apierr"
	"github.com/plat5dev/operator/internal/auth"
	"github.com/plat5dev/operator/internal/authorizer"
	"github.com/plat5dev/operator/internal/httpx"
	"github.com/plat5dev/operator/internal/identity"
)

const (
	maxAccessRoutesBodyBytes = 256 << 10
	maxAccessRoutesChecks    = 100
	maxCheckPathLen          = 2048
)

var allowedCheckMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true,
}

type accessRoutesRequest struct {
	Checks []accessRouteCheck `json:"checks"`
}

type accessRouteCheck struct {
	Method   string `json:"method"`
	Path     string `json:"path"`
	AsMember string `json:"as_member,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type accessRouteResult struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

func (g *Gateway) handleAccessRoutes(w http.ResponseWriter, r *http.Request, reqID, ip string, op *auth.Operator) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAccessRoutesBodyBytes+1))
	if err != nil {
		apierr.InvalidRequest(w, r)
		return
	}
	if len(body) > maxAccessRoutesBodyBytes {
		apierr.PayloadTooLarge(w, r)
		return
	}
	var req accessRoutesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		apierr.Validation(w, r, "body")
		return
	}
	if len(req.Checks) < 1 || len(req.Checks) > maxAccessRoutesChecks {
		apierr.Validation(w, r, "checks")
		return
	}
	for _, c := range req.Checks {
		if !allowedCheckMethods[c.Method] {
			apierr.Validation(w, r, "checks")
			return
		}
		if !validCheckPath(c.Path) {
			apierr.Validation(w, r, "checks")
			return
		}
		if c.AsMember != "" && !httpx.ValidSegmentID(strings.TrimSpace(c.AsMember), maxMemberIDLen) {
			apierr.Validation(w, r, "checks")
			return
		}
		if c.Reason != "" && !validReason(strings.TrimSpace(c.Reason)) {
			apierr.Validation(w, r, "checks")
			return
		}
	}

	results := make([]accessRouteResult, len(req.Checks))
	var batchIndices []int
	var batchItems []authorizer.BatchItem

	for i, c := range req.Checks {
		m, pathErr := g.registry.Resolve(c.Method, c.Path)
		if pathErr != nil || m == nil {
			results[i] = accessRouteResult{Allowed: false}
			continue
		}
		asMember := strings.TrimSpace(c.AsMember)
		reason := strings.TrimSpace(c.Reason)
		if asMember != "" && m.NodeID == "" {
			results[i] = accessRouteResult{Allowed: false}
			continue
		}
		res := g.resolve(r.Context(), reqID, m, asMember)
		if res.err != nil {
			if errors.Is(res.err, identity.ErrNotFound) || errors.Is(res.err, identity.ErrNotActive) {
				results[i] = accessRouteResult{Allowed: false}
				continue
			}
			g.log.Error("identity unavailable during /access/routes", "request_id", reqID, "err", res.err)
			apierr.Unavailable(w, r)
			return
		}
		batchIndices = append(batchIndices, i)
		batchItems = append(batchItems, authorizer.BatchItem{
			Action:   buildAuthZENAction(c.Method, m, asMember, res.principal, reason),
			Resource: buildAuthZENResource(m, res.found()),
		})
	}

	if len(batchItems) > 0 {
		if authz := g.currentAuthorizer(); authz == nil {
			for _, idx := range batchIndices {
				results[idx] = accessRouteResult{Allowed: true}
			}
		} else {
			evals, err := authz.EvaluateBatch(r.Context(), buildAuthZENSubject(op), map[string]any{
				"client_ip":  ip,
				"request_id": reqID,
			}, batchItems)
			if err != nil {
				g.log.Error("authorizer unavailable during /access/routes", "request_id", reqID, "err", err)
				apierr.Unavailable(w, r)
				return
			}
			for j, idx := range batchIndices {
				results[idx] = accessRouteResult{
					Allowed: evals[j].Allowed,
					Reason:  evals[j].Reason,
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"checks": results})
}

func validCheckPath(p string) bool {
	if len(p) == 0 || len(p) > maxCheckPathLen || p[0] != '/' {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] < 0x21 || p[i] > 0x7e || p[i] == '?' || p[i] == '#' {
			return false
		}
	}
	return true
}
