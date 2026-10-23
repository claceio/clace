// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/openrundev/openrun/internal/types"
	"golang.org/x/sync/singleflight"
)

const (
	mkcertTimeout     = 30 * time.Second
	mkcertFailureWait = time.Minute // a failed mkcert run is not retried for the domain within this window
)

// diskCertProvider is the TLS GetCertificate implementation used when ACME is
// disabled. It serves <domain>.crt/<domain>.key from the cert location, creating
// them using mkcert for local dev domains, and falls back to a default self signed
// cert. The SNI is client controlled and is read before any HTTP level checks, so
// it is used for file lookups only after it passes hostname validation and the
// allowed domain check
type diskCertProvider struct {
	*types.Logger
	config        func() *types.HttpsConfig
	mkcertPath    string
	mkcertDomains []string // https.mkcert_domains patterns, see parseMkcertDomains
	domainAllowed func(domain string) (bool, error)

	mkcertGroup    singleflight.Group
	failureMu      sync.Mutex
	mkcertFailures map[string]time.Time // domain -> time of the last failed mkcert run
}

func newDiskCertProvider(logger *types.Logger, config func() *types.HttpsConfig, mkcertPath string,
	domainAllowed func(domain string) (bool, error)) (*diskCertProvider, error) {
	mkcertDomains, err := parseMkcertDomains(config().MkcertDomains)
	if err != nil {
		return nil, err
	}
	return &diskCertProvider{
		Logger:         logger,
		config:         config,
		mkcertPath:     mkcertPath,
		mkcertDomains:  mkcertDomains,
		domainAllowed:  domainAllowed,
		mkcertFailures: map[string]time.Time{},
	}, nil
}

func (d *diskCertProvider) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	certDir := os.ExpandEnv(d.config().CertLocation)
	if hello.ServerName != "" && d.config().EnableCertLookup {
		if cert, ok := d.domainCertificate(certDir, hello.ServerName); ok {
			return cert, nil
		}
	}

	certFilePath := filepath.Join(certDir, DEFAULT_CERT_FILE)
	certKeyPath := filepath.Join(certDir, DEFAULT_KEY_FILE)
	if !certPairExists(certFilePath, certKeyPath) {
		d.Info().Msgf("Generating default self signed certificate")
		err := GenerateSelfSignedCertificate(certFilePath, certKeyPath, 365*24*time.Hour)
		if err != nil {
			return nil, fmt.Errorf("error generating self signed certificate: %w", err)
		}
	}

	cert, err := tls.LoadX509KeyPair(certFilePath, certKeyPath)
	return &cert, err
}

// domainCertificate returns the cert for the domain from the cert location,
// generating it using mkcert if required. ok is false if the default cert
// should be used instead
func (d *diskCertProvider) domainCertificate(certDir, serverName string) (*tls.Certificate, bool) {
	domain := strings.ToLower(serverName)
	if !isValidCertHostname(domain) {
		d.Debug().Msgf("Ignoring invalid TLS server name %q", serverName)
		return nil, false
	}
	allowed, err := d.domainAllowed(domain)
	if err != nil {
		d.Warn().Err(err).Msgf("error checking domain %s for certificate lookup", domain)
		return nil, false
	}
	if !allowed {
		d.Debug().Msgf("Ignoring TLS server name %s, not a configured domain", domain)
		return nil, false
	}

	certFilePath := filepath.Join(certDir, domain+".crt")
	certKeyPath := filepath.Join(certDir, domain+".key")
	if !pathWithin(certDir, certFilePath) || !pathWithin(certDir, certKeyPath) {
		// Not expected after hostname validation, checked as a safeguard
		d.Warn().Msgf("Ignoring TLS server name %q, cert path is outside cert location", serverName)
		return nil, false
	}

	if !certPairExists(certFilePath, certKeyPath) {
		if d.mkcertPath == "" || !d.isMkcertDomain(domain) || !d.generateMkcert(domain, certFilePath, certKeyPath) {
			return nil, false
		}
	}

	cert, err := tls.LoadX509KeyPair(certFilePath, certKeyPath)
	if err != nil {
		d.Warn().Err(err).Msgf("error loading certificate for domain %s", domain)
		return nil, false
	}
	return &cert, true
}

// generateMkcert creates the cert files for the domain using mkcert. Concurrent
// handshakes for a domain share one mkcert run, and a failed run is not retried
// until mkcertFailureWait has passed
func (d *diskCertProvider) generateMkcert(domain, certFilePath, certKeyPath string) bool {
	if d.recentMkcertFailure(domain) {
		return false
	}
	_, err, _ := d.mkcertGroup.Do(domain, func() (any, error) {
		if certPairExists(certFilePath, certKeyPath) {
			return nil, nil // created by a run which completed before this one started
		}
		d.Info().Msgf("Generating mkcert certificate for domain %s", domain)
		mkcertCtx, mkcertCancel := context.WithTimeout(context.Background(), mkcertTimeout)
		defer mkcertCancel()
		// "--" so that the domain is never parsed as a mkcert flag
		cmd := exec.CommandContext(mkcertCtx, d.mkcertPath, "-cert-file", certFilePath, "-key-file", certKeyPath, "--", domain)
		if output, err := cmd.CombinedOutput(); err != nil {
			d.recordMkcertFailure(domain)
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil, nil
	})
	if err != nil {
		d.Warn().Err(err).Msgf("error generating certificate using mkcert for domain %s, using default certificate", domain)
		return false
	}
	return certPairExists(certFilePath, certKeyPath)
}

func (d *diskCertProvider) recentMkcertFailure(domain string) bool {
	d.failureMu.Lock()
	defer d.failureMu.Unlock()
	failedAt, ok := d.mkcertFailures[domain]
	if ok && time.Since(failedAt) >= mkcertFailureWait {
		delete(d.mkcertFailures, domain)
		return false
	}
	return ok
}

func (d *diskCertProvider) recordMkcertFailure(domain string) {
	d.failureMu.Lock()
	defer d.failureMu.Unlock()
	d.mkcertFailures[domain] = time.Now()
}

func certPairExists(certFilePath, certKeyPath string) bool {
	_, certErr := os.Stat(certFilePath)
	_, keyErr := os.Stat(certKeyPath)
	return certErr == nil && keyErr == nil
}

// pathWithin returns true if target is a file under dir
func pathWithin(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// isValidCertHostname checks that the lowercased name is a DNS hostname: dot
// separated labels of letters, digits, '-' and '_', no label starting or ending
// with '-'. This rules out path separators, "..", and mkcert flag or URL syntax
func isValidCertHostname(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for label := range strings.SplitSeq(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' {
				return false
			}
		}
	}
	return true
}

// parseMkcertDomains validates and lowercases the https.mkcert_domains patterns:
// a hostname matches exactly, "*.<hostname>" matches any subdomain of it and "*"
// matches all domains. mkcert certs are trusted only on machines with the mkcert
// CA installed, so the default is local dev domains only
func parseMkcertDomains(patterns []string) ([]string, error) {
	parsed := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern != "*" && !isValidCertHostname(strings.TrimPrefix(pattern, "*.")) {
			return nil, fmt.Errorf("invalid https.mkcert_domains entry %q, expected a domain, *.<domain> or *", pattern)
		}
		parsed = append(parsed, pattern)
	}
	return parsed, nil
}

func (d *diskCertProvider) isMkcertDomain(domain string) bool {
	for _, pattern := range d.mkcertDomains {
		if pattern == "*" || pattern == domain {
			return true
		}
		if suffix, ok := strings.CutPrefix(pattern, "*"); ok && strings.HasSuffix(domain, suffix) {
			return true
		}
	}
	return false
}
