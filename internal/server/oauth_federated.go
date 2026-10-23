// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/openrundev/openrun/internal/system"
	"github.com/openrundev/openrun/internal/types"
)

// Federated login for the OAuth authorization server: when a resource's
// login mechanism is an [auth.*] provider or a [saml.*] provider, the
// authorize page cannot collect a password. Instead the pending authorize
// request is stored (metadata KV, multi-node safe), the browser is sent
// through the existing provider / SAML login flow with a return URL on the
// AS host, the provider session cookie set by that flow identifies the user
// on return, and a session-backed consent page (nonce-protected) issues the
// code. The provider callback already records the identity row and group
// snapshot (observeFederatedIdentity), so the token verifier needs nothing
// new.
//
// The browser pages of the flow (the authorize login/chooser page, the
// continue step and the consent page) are served only on the login origin:
// the reserved auth callback domain (security.auth_callback_domain), on
// which no app is ever served. The continue and consent handlers identify
// the user from the provider session cookie, so on a host that also serves
// apps, app JavaScript could fetch the consent form with that cookie, read
// its nonce and post it back, minting a code the user never saw. On the
// login origin that code is always cross-origin. The continue URL is built
// from config, never from the request Host header: it is where the
// provider callback sends the single-use completion token, so a Host an
// attacker named must not become its destination.

const (
	oauthAuthzTTL      = 10 * time.Minute
	oauthAuthzKVPrefix = "oauth_authz:"
)

// oauthLoginOrigin returns the origin serving the AS browser pages: the
// issuer's scheme and port on the reserved auth callback domain. ok is
// false when none is usable, see loginOriginError
func (s *Server) oauthLoginOrigin() (string, bool) {
	origin, err := s.loginOriginError()
	return origin, err == nil
}

// loginOriginError resolves the login origin or explains why there is
// none: no auth domain (security.auth_callback_domain empty, or the "auth."
// prefix with no default_domain), no issuer, or a plaintext issuer with a
// non-local auth domain. The last case matters because an http issuer is
// only served to loopback callers (mcpTransportAllowed, the sessionHttpsOnly
// exception of the login form): http://login.example.com would be
// advertised but never answer, so it is treated as no login origin and the
// federated logins are refused with this reason
func (s *Server) loginOriginError() (string, error) {
	if s.formLogin == nil {
		return "", errors.New("the login form is not initialized")
	}
	authDomain, ok := s.formLogin.reservedAuthDomain()
	if !ok {
		return "", errors.New("set security.auth_callback_domain (default \"auth.\", giving auth.<system.default_domain>) " +
			"so the login and consent pages are served on a domain with no apps")
	}
	parsed, err := url.Parse(s.apiExternalUrl())
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("the remote API has no external url (api.external_url)")
	}
	if parsed.Scheme != "https" && !isLocalhostHost(authDomain) {
		return "", fmt.Errorf("the issuer %s is plaintext, which is served to loopback callers only, "+
			"but the auth callback domain %s is not local: use an https api.external_url or a *.localhost auth domain",
			s.apiExternalUrl(), authDomain)
	}
	host := authDomain
	if port := parsed.Port(); port != "" {
		host = net.JoinHostPort(authDomain, port)
	}
	return parsed.Scheme + "://" + host, nil
}

// oauthContinueUrl is the return point of a federated login on the login
// origin, built from config only (see the package comment above)
func oauthContinueUrl(origin, id string) string {
	return origin + types.INTERNAL_URL_PREFIX + "/oauth/authorize/continue?authz=" + url.QueryEscape(id)
}

// federatedLoginUnavailable returns the message shown in place of the
// federated mechanisms when no login origin resolves, "" when one does
func (s *Server) federatedLoginUnavailable() string {
	if _, err := s.loginOriginError(); err != nil {
		return "federated login needs the auth callback domain: " + err.Error()
	}
	return ""
}

// warnIfFederatedLoginUnavailable logs, at startup and on config reload,
// when an enabled remote surface lists a federated login mechanism that
// will be refused because no login origin resolves (loginOriginError)
func (s *Server) warnIfFederatedLoginUnavailable() {
	_, err := s.loginOriginError()
	if err == nil {
		return
	}
	config := s.Config()
	for _, surface := range []string{ApiResourceRest, ApiResourceMCP} {
		surfaceConfig, ok := config.Api.Surface(surface)
		if !ok || !surfaceConfig.Enable {
			continue
		}
		for _, mechanism := range surfaceConfig.Auth {
			if !isPasswordMechanism(mechanism) {
				s.Warn().Str("surface", surface).Str("mechanism", mechanism).Msgf(
					"federated login for the remote API will be refused: %s", err)
			}
		}
	}
}

// isOAuthLoginHost reports whether the request arrived on the login origin host
func (h *Handler) isOAuthLoginHost(r *http.Request) bool {
	return h.server.formLogin != nil && h.server.formLogin.isReservedAuthHost(system.GetHostname(r.Host))
}

// oauthLoginHostOnly serves a cookie-reading AS page only on the login
// origin host, a 404 anywhere else (including everywhere when no login
// origin resolves: the federated flow that leads here is then refused at
// its start, see startFederatedLogin)
func (h *Handler) oauthLoginHostOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := h.server.oauthLoginOrigin(); !ok || !h.isOAuthLoginHost(r) {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

// oauthLoginHostRedirect sends a GET for an AS page on any other host to the
// same path on the login origin (a client that hard-coded the issuer path
// rather than reading the metadata, which names the login origin). The
// page itself is served when no login origin resolves
func (h *Handler) oauthLoginHostRedirect(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if origin, ok := h.server.oauthLoginOrigin(); ok && !h.isOAuthLoginHost(r) {
			http.Redirect(w, r, origin+r.URL.RequestURI(), http.StatusFound)
			return
		}
		next(w, r)
	}
}

// oauthAuthzRequest is a pending authorize request awaiting a federated
// login and consent
type oauthAuthzRequest struct {
	ClientId     string    `json:"client_id"`
	RedirectUri  string    `json:"redirect_uri"`
	State        string    `json:"state"`
	Challenge    string    `json:"code_challenge"`
	Method       string    `json:"code_challenge_method"`
	Resource     string    `json:"resource"`
	Scope        string    `json:"scope"`
	ResponseType string    `json:"response_type"`
	Mechanism    string    `json:"mechanism"` // the provider or saml_<name> chosen
	Nonce        string    `json:"nonce"`     // consent form token, bound to this record
	Expires      time.Time `json:"expires"`
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Server) storeOAuthAuthz(ctx context.Context, id string, req *oauthAuthzRequest) error {
	blob, err := json.Marshal(req)
	if err != nil {
		return err
	}
	expires := req.Expires.UTC()
	return s.db.StoreKVBlob(ctx, oauthAuthzKVPrefix+id, blob, &expires)
}

// fetchOAuthAuthz returns the pending request, nil when unknown or expired
func (s *Server) fetchOAuthAuthz(ctx context.Context, id string) (*oauthAuthzRequest, error) {
	if id == "" {
		return nil, nil
	}
	blob, err := s.db.FetchKVBlob(ctx, oauthAuthzKVPrefix+id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var req oauthAuthzRequest
	if err := json.Unmarshal(blob, &req); err != nil {
		return nil, err
	}
	if time.Now().After(req.Expires) {
		return nil, nil
	}
	return &req, nil
}

// consumeOAuthAuthz removes the record; single use across nodes
func (s *Server) consumeOAuthAuthz(ctx context.Context, id string) (bool, error) {
	return s.db.DeleteKVIfPresent(ctx, oauthAuthzKVPrefix+id)
}

// isPasswordMechanism: mechanisms the authorize page authenticates itself
func isPasswordMechanism(mechanism string) bool {
	return mechanism == "builtin" || mechanism == "admin"
}

// federatedMechanisms returns the configured federated mechanisms of a
// resource (provider names and saml_<name>), skipping unknown entries with
// a warning
func (s *Server) federatedMechanisms(res *oauthResource) []string {
	mechanisms, err := s.oauthLoginMechanisms(res)
	if err != nil {
		return nil
	}
	federated := make([]string, 0, len(mechanisms))
	for _, mechanism := range mechanisms {
		if isPasswordMechanism(mechanism) {
			continue
		}
		if s.validFederatedMechanism(mechanism) {
			federated = append(federated, mechanism)
		} else {
			s.Warn().Msgf("login mechanism %q for resource %s is not a configured auth or saml provider", mechanism, res.URI)
		}
	}
	return federated
}

func (s *Server) validFederatedMechanism(mechanism string) bool {
	if strings.HasPrefix(mechanism, SAML_AUTH_PREFIX) {
		return s.samlManager != nil && s.samlManager.ValidateSAMLProvider(mechanism)
	}
	return s.oAuthManager != nil && s.oAuthManager.ValidateProviderName(mechanism)
}

// hasPasswordMechanism reports whether the resource accepts the password
// form (builtin or admin)
func (s *Server) hasPasswordMechanism(res *oauthResource) bool {
	mechanisms, err := s.oauthLoginMechanisms(res)
	if err != nil {
		return false
	}
	for _, mechanism := range mechanisms {
		if isPasswordMechanism(mechanism) {
			return true
		}
	}
	return false
}

// mechanismLabel names a mechanism on the chooser
func mechanismLabel(mechanism string) string {
	if name, ok := strings.CutPrefix(mechanism, SAML_AUTH_PREFIX); ok {
		return name + " (SAML)"
	}
	return mechanism
}

// startFederatedLogin stores the authorize request and sends the browser
// into the provider or SAML login with a return URL on the AS host
func (h *Handler) startFederatedLogin(w http.ResponseWriter, r *http.Request, get func(string) string, mechanism string) {
	s := h.server
	res, err := h.oauthValidateAuthorizeParams(r.Context(), get("client_id"), get("redirect_uri"),
		get("code_challenge"), get("code_challenge_method"), get("resource"))
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if get("response_type") != "code" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_response_type", "only response_type=code is supported")
		return
	}
	allowed := false
	for _, configured := range s.federatedMechanisms(res) {
		if configured == mechanism {
			allowed = true
		}
	}
	if !allowed {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("login mechanism %q is not available for this resource", mechanism))
		return
	}
	origin, ok := s.oauthLoginOrigin()
	if !ok {
		// No app-free origin to run the cookie-based steps on: refuse rather
		// than run them on the issuer host, where apps may be served
		s.Warn().Msg(s.federatedLoginUnavailable())
		h.renderOAuthLogin(w, r, get, s.federatedLoginUnavailable())
		return
	}
	id, err := randomHex(16)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	nonce, err := randomHex(16)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	if err := s.storeOAuthAuthz(r.Context(), id, &oauthAuthzRequest{
		ClientId: get("client_id"), RedirectUri: get("redirect_uri"), State: get("state"),
		Challenge: get("code_challenge"), Method: get("code_challenge_method"), Resource: get("resource"),
		Scope: get("scope"), ResponseType: get("response_type"), Mechanism: mechanism, Nonce: nonce,
		Expires: time.Now().Add(oauthAuthzTTL),
	}); err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "error storing authorization request")
		return
	}
	// The login flow returns to the login origin; the provider session
	// cookie is then set on that host and read by the continue handler
	returnUrl := oauthContinueUrl(origin, id)
	if strings.HasPrefix(mechanism, SAML_AUTH_PREFIX) {
		s.samlManager.login(w, r, mechanism, returnUrl)
		return
	}
	s.oAuthManager.beginLogin(w, r, mechanism, returnUrl)
}

// oauthAuthorizeFederated is the chooser's POST: start the selected login
func (h *Handler) oauthAuthorizeFederated(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	h.startFederatedLogin(w, r, r.PostForm.Get, r.PostForm.Get("mechanism"))
}

// federatedIdentity reads the user from the provider/SAML session cookie
// on the login origin. Without a valid session the browser is sent into
// the login, returning to returnUrl (the config-built continue URL, never
// the request URL). Returns done=true when a response (login redirect or
// error) was already written
func (h *Handler) federatedIdentity(w http.ResponseWriter, r *http.Request, mechanism, returnUrl string) (principal string, groups []string, done bool) {
	s := h.server
	if strings.HasPrefix(mechanism, SAML_AUTH_PREFIX) {
		principal, groups, err := s.samlManager.checkSAMLAuth(w, r, mechanism, returnUrl)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return "", nil, true
		}
		return principal, groups, principal == ""
	}
	info, err := s.oAuthManager.checkAuthInfo(w, r, mechanism, returnUrl)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return "", nil, true
	}
	return info.UserId, info.Groups, info.UserId == ""
}

// loadOAuthAuthz fetches and re-validates a pending request by id; a nil
// result means an error response was written
func (h *Handler) loadOAuthAuthz(w http.ResponseWriter, r *http.Request, id string) (*oauthAuthzRequest, *oauthResource) {
	authz, err := h.server.fetchOAuthAuthz(r.Context(), id)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "error reading authorization request")
		return nil, nil
	}
	if authz == nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "unknown or expired authorization request, start the login again")
		return nil, nil
	}
	get := func(name string) string {
		switch name {
		case "client_id":
			return authz.ClientId
		case "redirect_uri":
			return authz.RedirectUri
		case "code_challenge":
			return authz.Challenge
		case "code_challenge_method":
			return authz.Method
		case "resource":
			return authz.Resource
		}
		return ""
	}
	res, err := h.oauthValidateAuthorizeParams(r.Context(), get("client_id"), get("redirect_uri"),
		get("code_challenge"), get("code_challenge_method"), get("resource"))
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return nil, nil
	}
	return authz, res
}

// oauthAuthorizeContinue is the return point of the federated login: read
// the identity from the session cookie and render consent. Served on the
// login origin only (oauthLoginHostOnly), re-checked here
func (h *Handler) oauthAuthorizeContinue(w http.ResponseWriter, r *http.Request) {
	origin, ok := h.server.oauthLoginOrigin()
	if !ok || !h.isOAuthLoginHost(r) {
		http.NotFound(w, r)
		return
	}
	id := r.URL.Query().Get("authz")
	authz, res := h.loadOAuthAuthz(w, r, id)
	if authz == nil {
		return
	}
	principal, groups, done := h.federatedIdentity(w, r, authz.Mechanism, oauthContinueUrl(origin, id))
	if done {
		return
	}
	h.renderOAuthConsent(w, r, id, authz, res, principal, groups, "")
}

func (h *Handler) renderOAuthConsent(w http.ResponseWriter, r *http.Request, id string, authz *oauthAuthzRequest,
	res *oauthResource, principal string, groups []string, errMsg string) {
	scope := strings.Join(h.server.oauthGrantScopes(res, parseScopeParam(authz.Scope)), " ")
	clientName, cimdClient, loopbackOnly := h.oauthClientDisplay(r.Context(), authz.ClientId)
	h.server.formLogin.renderOAuthPage(w, "oauth_consent.go.html", map[string]any{
		"Principal":     principal,
		"ClientName":    clientName,
		"Resource":      res.Label(),
		"Scope":         scope,
		"RedirectUri":   authz.RedirectUri,
		"ClientId":      authz.ClientId,
		"DynamicClient": authz.ClientId != oauthCLIClientId && !cimdClient,
		"CIMDClient":    cimdClient,
		"LoopbackOnly":  loopbackOnly,
		"Error":         errMsg,
		"Action":        types.INTERNAL_URL_PREFIX + "/oauth/authorize/consent",
		"Authz":         id,
		"Nonce":         authz.Nonce,
	})
}

// oauthAuthorizeConsent issues the code for a federated login: the record
// is single use, the nonce binds the form to it, and the identity is read
// again from the session cookie (never from the form). Served on the
// login origin only (oauthLoginHostOnly), re-checked here
func (h *Handler) oauthAuthorizeConsent(w http.ResponseWriter, r *http.Request) {
	origin, ok := h.server.oauthLoginOrigin()
	if !ok || !h.isOAuthLoginHost(r) {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	id := r.PostForm.Get("authz")
	authz, res := h.loadOAuthAuthz(w, r, id)
	if authz == nil {
		return
	}
	if nonce := r.PostForm.Get("nonce"); nonce == "" || subtle.ConstantTimeCompare([]byte(nonce), []byte(authz.Nonce)) != 1 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "consent form token mismatch")
		return
	}
	principal, groups, done := h.federatedIdentity(w, r, authz.Mechanism, oauthContinueUrl(origin, id))
	if done {
		return
	}
	// Recoverable problems (a bad scope, no app access) are shown on the
	// form with the record intact, so the user can correct and resubmit;
	// only a valid consent consumes the single-use record
	scopes, errMsg, err := h.validateOAuthConsent(r, res, principal, groups, parseScopeParam(r.PostForm.Get("or_scope")))
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	if errMsg != "" {
		h.renderOAuthConsent(w, r, id, authz, res, principal, groups, errMsg)
		return
	}
	consumed, err := h.server.consumeOAuthAuthz(r.Context(), id)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "error consuming authorization request")
		return
	}
	if !consumed {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "authorization request already used")
		return
	}
	h.issueOAuthCode(w, r, res, principal, scopes, authz.ClientId, authz.RedirectUri, authz.Challenge, authz.State)
}
