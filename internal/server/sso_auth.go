// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"crypto/rand"
	"fmt"
	"io"
	"net/http"

	"github.com/claceio/clace/internal/utils"
	"github.com/go-chi/chi"
	"github.com/gorilla/sessions"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"

	"github.com/markbates/goth/providers/amazon"
	"github.com/markbates/goth/providers/auth0"
	"github.com/markbates/goth/providers/azuread"
	"github.com/markbates/goth/providers/bitbucket"
	"github.com/markbates/goth/providers/digitalocean"
	"github.com/markbates/goth/providers/github"
	"github.com/markbates/goth/providers/gitlab"
	"github.com/markbates/goth/providers/google"
	"github.com/markbates/goth/providers/microsoftonline"
	"github.com/markbates/goth/providers/okta"
	"github.com/markbates/goth/providers/openidConnect"
)

const (
	SESSION_COOKIE = "clace_session"
	AUTH_KEY       = "authenticated"
	USER_ID_KEY    = "user"
	USER_EMAIL_KEY = "email"
	REDIRECT_URL   = "redirect"
)

type SSOAuth struct {
	*utils.Logger
	config              *utils.ServerConfig
	cookieStore         *sessions.CookieStore
	configuredProviders map[string]bool
}

func NewSSOAuth(logger *utils.Logger, config *utils.ServerConfig) *SSOAuth {
	return &SSOAuth{
		Logger: logger,
		config: config,
	}
}

func getProviderName(r *http.Request) (string, error) {
	provider := chi.URLParam(r, "provider")
	if provider == "" {
		return "", fmt.Errorf("provider not specified in url")
	}
	return provider, nil
}

func (s *SSOAuth) Setup() error {
	sessionKey := s.config.Security.SessionSecret
	if sessionKey == "" {
		k := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, k); err != nil {
			return err
		}
	}

	s.cookieStore = sessions.NewCookieStore([]byte(sessionKey))
	s.cookieStore.MaxAge(s.config.Security.SessionMaxAge)
	s.cookieStore.Options.Path = "/"
	s.cookieStore.Options.HttpOnly = true
	s.cookieStore.Options.Secure = s.config.Security.SessionHttpsOnly

	gothic.Store = s.cookieStore // Set the store for gothic
	gothic.GetProviderName = getProviderName

	providers := make([]goth.Provider, 0)
	for provider, auth := range s.config.Auth {
		key := auth.Key
		secret := auth.Secret
		scopes := auth.Scopes

		if provider == "" || key == "" || secret == "" {
			return fmt.Errorf("provider, key, and secret must be set for each auth provider")
		}

		callbackUrl := s.config.Security.CallbackUrl + utils.INTERNAL_URL_PREFIX + "/auth/" + provider + "/callback"

		switch provider {
		case "github":
			providers = append(providers, github.New(key, secret, callbackUrl, scopes...))
		case "google":
			providers = append(providers, google.New(key, secret, callbackUrl, scopes...))
		case "digitalocean":
			providers = append(providers, digitalocean.New(key, secret, callbackUrl, scopes...))
		case "bitbucket":
			providers = append(providers, bitbucket.New(key, secret, callbackUrl, scopes...))
		case "amazon":
			providers = append(providers, amazon.New(key, secret, callbackUrl, scopes...))
		case "azuread": // azuread requires a resources array, setting nil for now
			providers = append(providers, azuread.New(key, secret, callbackUrl, nil, scopes...))
		case "microsoftonline":
			providers = append(providers, microsoftonline.New(key, secret, callbackUrl, scopes...))
		case "gitlab":
			providers = append(providers, gitlab.New(key, secret, callbackUrl, scopes...))
		case "auth0": // auth0 requires a domain
			providers = append(providers, auth0.New(key, secret, callbackUrl, auth.Domain, scopes...))
		case "okta": // okta requires an org url
			providers = append(providers, okta.New(key, secret, callbackUrl, auth.OrgUrl, scopes...))
		case "oidc": // openidConnect requires a discovery url
			provider, err := openidConnect.New(key, secret, callbackUrl, auth.DiscoveryUrl, scopes...)
			if err != nil {
				return fmt.Errorf("failed to create OIDC provider: %w", err)
			}
			providers = append(providers, provider)
		default:
			return fmt.Errorf("unsupported auth provider: %s", provider)
		}
	}

	if len(providers) != 0 && s.config.Security.CallbackUrl == "" {
		return fmt.Errorf("security.callback_url must be set for enabling SSO auth")
	}

	s.configuredProviders = make(map[string]bool)
	for _, provider := range providers {
		s.configuredProviders[provider.Name()] = true
	}

	goth.UseProviders(providers...) // Register the providers with goth
	return nil
}

func (s *SSOAuth) RegisterRoutes(mux *chi.Mux) {
	mux.Get(utils.INTERNAL_URL_PREFIX+"/auth/{provider}/callback", func(w http.ResponseWriter, r *http.Request) {
		user, err := gothic.CompleteUserAuth(w, r)
		if err != nil {
			fmt.Fprintln(w, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Set user as authenticated in session
		session, err := s.cookieStore.Get(r, SESSION_COOKIE)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		session.Values[AUTH_KEY] = true
		session.Values[USER_ID_KEY] = user.UserID
		session.Values[USER_EMAIL_KEY] = user.Email
		session.Save(r, w)

		// Redirect to the original page, or default to the home page if not specified
		redirectTo, ok := session.Values[REDIRECT_URL].(string)
		if !ok || redirectTo == "" {
			redirectTo = "/"
		}

		http.Redirect(w, r, redirectTo, http.StatusTemporaryRedirect)
	})

	mux.Get(utils.INTERNAL_URL_PREFIX+"/logout/{provider}", func(w http.ResponseWriter, r *http.Request) {
		gothic.Logout(w, r)
		// Set user as authenticated in session
		session, err := s.cookieStore.Get(r, SESSION_COOKIE)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Set user as unauthenticated in session
		session.Values[AUTH_KEY] = false
		session.Save(r, w)

		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
	})

	mux.Get(utils.INTERNAL_URL_PREFIX+"/auth/{provider}", func(w http.ResponseWriter, r *http.Request) {
		// try to get the user without re-authenticating
		if _, err := gothic.CompleteUserAuth(w, r); err == nil {
			session, err := s.cookieStore.Get(r, SESSION_COOKIE)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Redirect to the original page, or default to the home page if not specified
			redirectTo, ok := session.Values[REDIRECT_URL].(string)
			if !ok || redirectTo == "" {
				redirectTo = "/"
			}

			http.Redirect(w, r, redirectTo, http.StatusTemporaryRedirect)
		} else {
			// Start login process
			gothic.BeginAuthHandler(w, r)
		}
	})
}

func (s *SSOAuth) VerifyProvider(provider string) bool {
	return s.configuredProviders[provider]
}

func (s *SSOAuth) ValidateAuthType(authType string) bool {
	switch authType {
	case string(utils.AppAuthnDefault), string(utils.AppAuthnSystem), string(utils.AppAuthnNone):
		return true
	default:
		return s.VerifyProvider(authType)
	}
}

func (s *SSOAuth) CheckAuth(w http.ResponseWriter, r *http.Request, provider string) (bool, error) {
	session, err := s.cookieStore.Get(r, SESSION_COOKIE)
	if err != nil {
		return false, err
	}
	if auth, ok := session.Values[AUTH_KEY].(bool); !ok || !auth {
		// Store the target URL before redirecting to login
		session.Values[REDIRECT_URL] = r.RequestURI
		session.Save(r, w)
		http.Redirect(w, r, utils.INTERNAL_URL_PREFIX+"/auth/"+provider, http.StatusTemporaryRedirect)
		return false, nil
	}

	// Clear the redirect target after successful authentication
	delete(session.Values, REDIRECT_URL)
	session.Save(r, w)
	return true, nil
}
