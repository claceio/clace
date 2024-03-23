// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"strings"

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
	PROVIDER_NAME_DELIMITER = "_"
	SESSION_COOKIE          = "clace_session"
	AUTH_KEY                = "authenticated"
	USER_ID_KEY             = "user"
	USER_EMAIL_KEY          = "email"
	REDIRECT_URL            = "redirect"
)

type SSOAuth struct {
	*utils.Logger
	config          *utils.ServerConfig
	cookieStore     *sessions.CookieStore
	providerConfigs map[string]*utils.AuthConfig
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
	s.providerConfigs = make(map[string]*utils.AuthConfig)

	providers := make([]goth.Provider, 0)
	for providerName, auth := range s.config.Auth {
		key := auth.Key
		secret := auth.Secret
		scopes := auth.Scopes

		if providerName == "" || key == "" || secret == "" {
			return fmt.Errorf("provider, key, and secret must be set for each auth provider")
		}

		callbackUrl := s.config.Security.CallbackUrl + utils.INTERNAL_URL_PREFIX + "/auth/" + providerName + "/callback"

		providerSplit := strings.SplitN(providerName, PROVIDER_NAME_DELIMITER, 2)
		providerType := providerSplit[0]

		var provider goth.Provider
		switch providerType {
		case "github":
			provider = github.New(key, secret, callbackUrl, scopes...)
		case "google": // google supports hosted domain option
			gp := google.New(key, secret, callbackUrl, scopes...)
			if auth.HostedDomain != "" {
				gp.SetHostedDomain(auth.HostedDomain)
			}
			provider = gp
		case "digitalocean":
			provider = digitalocean.New(key, secret, callbackUrl, scopes...)
		case "bitbucket":
			provider = bitbucket.New(key, secret, callbackUrl, scopes...)
		case "amazon":
			provider = amazon.New(key, secret, callbackUrl, scopes...)
		case "azuread": // azuread requires a resources array, setting nil for now
			provider = azuread.New(key, secret, callbackUrl, nil, scopes...)
		case "microsoftonline":
			provider = microsoftonline.New(key, secret, callbackUrl, scopes...)
		case "gitlab":
			provider = gitlab.New(key, secret, callbackUrl, scopes...)
		case "auth0": // auth0 requires a domain
			provider = auth0.New(key, secret, callbackUrl, auth.Domain, scopes...)
		case "okta": // okta requires an org url
			provider = okta.New(key, secret, callbackUrl, auth.OrgUrl, scopes...)
		case "oidc": // openidConnect requires a discovery url
			op, err := openidConnect.New(key, secret, callbackUrl, auth.DiscoveryUrl, scopes...)
			if err != nil {
				return fmt.Errorf("failed to create OIDC provider: %w", err)
			}
			provider = op
		default:
			return fmt.Errorf("unsupported auth provider: %s", providerName)
		}

		provider.SetName(providerName)
		providers = append(providers, provider)
		s.providerConfigs[providerName] = &auth
	}

	if len(providers) != 0 && s.config.Security.CallbackUrl == "" {
		return fmt.Errorf("security.callback_url must be set for enabling SSO auth")
	}

	goth.UseProviders(providers...) // Register the providers with goth
	return nil
}

func (s *SSOAuth) validateResponse(providerName string, user goth.User) error {
	providerConfig := s.providerConfigs[providerName]
	if providerConfig == nil {
		return fmt.Errorf("provider %s not configured", providerName)
	}

	providerType := strings.SplitN(providerName, PROVIDER_NAME_DELIMITER, 2)[0]
	switch providerType {
	case "google":
		if providerConfig.HostedDomain != "" && user.RawData["hd"] != providerConfig.HostedDomain {
			return fmt.Errorf("user does not belong to the required hosted domain. Found %s, expected %s",
				user.RawData["hd"], providerConfig.HostedDomain)
		}
	}

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

		providerName := chi.URLParam(r, "provider")
		if err := s.validateResponse(providerName, user); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
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
	return s.providerConfigs[provider] != nil
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
