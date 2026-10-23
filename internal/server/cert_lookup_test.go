// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"crypto/tls"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openrundev/openrun/internal/types"
)

func TestIsValidCertHostname(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"localhost", true},
		{"app.localhost", true},
		{"a-b.example.com", true},
		{"_acme.example.com", true},
		{"x1.test", true},
		{"", false},
		{"-install", false},
		{"-cert-file=/tmp/x", false},
		{"a-.example.com", false},
		{"a..b", false},
		{".example.com", false},
		{"https://x/../../../evil", false},
		{"x/../../evil", false},
		{"..", false},
		{"a b", false},
		{"a@b.com", false},
		{"a\\b", false},
		{"a\x00b", false},
		{strings.Repeat("a", 64) + ".com", false},
		{strings.Repeat("a.", 127) + "com", false},
	}
	for _, tc := range tests {
		if got := isValidCertHostname(tc.name); got != tc.want {
			t.Errorf("isValidCertHostname(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

var defaultMkcertDomains = []string{"localhost", "*.localhost", "*.test", "*.local", "*.internal", "*.home.arpa"}

func TestIsMkcertDomain(t *testing.T) {
	tests := []struct {
		patterns []string
		domain   string
		want     bool
	}{
		{defaultMkcertDomains, "localhost", true},
		{defaultMkcertDomains, "app.localhost", true},
		{defaultMkcertDomains, "a.b.localhost", true},
		{defaultMkcertDomains, "app.test", true},
		{defaultMkcertDomains, "app.local", true},
		{defaultMkcertDomains, "svc.internal", true},
		{defaultMkcertDomains, "nas.home.arpa", true},
		{defaultMkcertDomains, "example.com", false},
		{defaultMkcertDomains, "localhost.com", false},
		{defaultMkcertDomains, "applocalhost", false},
		{defaultMkcertDomains, "test", false},
		{defaultMkcertDomains, "app.localhost.evil", false},
		{[]string{"Dev.Example.com"}, "dev.example.com", true},
		{[]string{"dev.example.com"}, "app.dev.example.com", false},
		{[]string{"*.dev.example.com"}, "app.dev.example.com", true},
		{[]string{"*.dev.example.com"}, "dev.example.com", false},
		{[]string{"*.dev.example.com"}, "appdev.example.com", false},
		{[]string{"*"}, "example.com", true},
		{[]string{}, "localhost", false},
	}
	for _, tc := range tests {
		patterns, err := parseMkcertDomains(tc.patterns)
		if err != nil {
			t.Fatalf("parseMkcertDomains(%q) failed: %v", tc.patterns, err)
		}
		d := &diskCertProvider{mkcertDomains: patterns}
		if got := d.isMkcertDomain(tc.domain); got != tc.want {
			t.Errorf("isMkcertDomain(%q) with %q = %v, want %v", tc.domain, tc.patterns, got, tc.want)
		}
	}

	for _, invalid := range []string{"", "*.", "**.localhost", "*localhost", "a/b", "-x.localhost", "*.*.localhost"} {
		if _, err := parseMkcertDomains([]string{invalid}); err == nil {
			t.Errorf("parseMkcertDomains(%q) should fail", invalid)
		}
	}
}

func TestCertDomainAllowed(t *testing.T) {
	config := &types.ServerConfig{}
	config.System.DefaultDomain = "example.com"
	config.System.RootServeListApps = "apps.example.com"
	config.Security.CallbackUrl = "https://callback.example.com:25223/"
	config.Api.ExternalUrl = "https://api.example.com"
	s := &Server{staticConfig: config}
	s.apps = &AppStore{allDomains: map[string]bool{"app.example.com": true}}

	for name, want := range map[string]bool{
		"example.com":          true,
		"localhost":            true,
		"apps.example.com":     true,
		"Callback.example.com": true,
		"api.example.com":      true,
		"app.example.com":      true,
		"other.example.com":    false,
		"":                     false,
	} {
		got, err := s.certDomainAllowed(name)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("certDomainAllowed(%q) = %v, want %v", name, got, want)
		}
	}

	// auto/disable are list_apps modes, not host names
	for _, mode := range []string{"auto", "disable"} {
		config.System.RootServeListApps = mode
		if got, _ := s.certDomainAllowed(mode); got {
			t.Errorf("certDomainAllowed(%q) should be false for root_serve_list_apps mode", mode)
		}
	}
}

type certLookupFixture struct {
	t        *testing.T
	root     string
	certDir  string
	logFile  string
	provider *diskCertProvider
	https    *types.HttpsConfig
	mkcert   []byte // leaf cert the fake mkcert creates
}

// newCertLookupFixture sets up a provider with a fake mkcert script, which logs
// its arguments and copies a pre-generated cert pair to the requested paths
func newCertLookupFixture(t *testing.T, allowed []string, mkcertScript string) *certLookupFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake mkcert is a shell script")
	}
	root := t.TempDir()
	f := &certLookupFixture{
		t:       t,
		root:    root,
		certDir: filepath.Join(root, "config", "certificates"),
		logFile: filepath.Join(root, "mkcert.log"),
	}
	if err := os.MkdirAll(f.certDir, 0700); err != nil {
		t.Fatal(err)
	}

	tplCert, tplKey := filepath.Join(root, "tpl.crt"), filepath.Join(root, "tpl.key")
	if err := GenerateSelfSignedCertificate(tplCert, tplKey, time.Hour); err != nil {
		t.Fatal(err)
	}
	f.mkcert = loadLeaf(t, tplCert, tplKey)

	if mkcertScript == "" {
		mkcertScript = `cp "$TPL_CERT" "$2"; cp "$TPL_KEY" "$4"`
	}
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> '" + f.logFile + "'\n" +
		"TPL_CERT='" + tplCert + "'\nTPL_KEY='" + tplKey + "'\n" +
		mkcertScript + "\n"
	mkcertPath := filepath.Join(root, "mkcert")
	if err := os.WriteFile(mkcertPath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}

	f.https = &types.HttpsConfig{CertLocation: f.certDir, EnableCertLookup: true, MkcertDomains: defaultMkcertDomains}
	allowedSet := map[string]bool{}
	for _, d := range allowed {
		allowedSet[d] = true
	}
	logger := types.NewLogger(&types.LogConfig{Level: "WARN"})
	var err error
	f.provider, err = newDiskCertProvider(logger, func() *types.HttpsConfig { return f.https }, mkcertPath,
		func(domain string) (bool, error) { return allowedSet[domain], nil })
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func loadLeaf(t *testing.T, certPath, keyPath string) []byte {
	t.Helper()
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	return cert.Certificate[0]
}

func (f *certLookupFixture) get(serverName string) []byte {
	f.t.Helper()
	cert, err := f.provider.GetCertificate(&tls.ClientHelloInfo{ServerName: serverName})
	if err != nil {
		f.t.Fatalf("GetCertificate(%q) failed: %v", serverName, err)
	}
	return cert.Certificate[0]
}

func (f *certLookupFixture) defaultLeaf() []byte {
	return loadLeaf(f.t, filepath.Join(f.certDir, DEFAULT_CERT_FILE), filepath.Join(f.certDir, DEFAULT_KEY_FILE))
}

func (f *certLookupFixture) mkcertCalls() []string {
	data, err := os.ReadFile(f.logFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func (f *certLookupFixture) certFiles() []string {
	var files []string
	_ = filepath.WalkDir(f.root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(p, ".crt") || strings.HasSuffix(p, ".key")) {
			rel, _ := filepath.Rel(f.root, p)
			files = append(files, rel)
		}
		return nil
	})
	return files
}

func TestDiskCertProviderRejectsUntrustedServerNames(t *testing.T) {
	f := newCertLookupFixture(t, []string{"app.localhost"}, "")

	// A valid pair outside the cert location, which a traversal would load
	outside := filepath.Join(f.root, "outside")
	if err := GenerateSelfSignedCertificate(outside+".crt", outside+".key", time.Hour); err != nil {
		t.Fatal(err)
	}

	for _, sni := range []string{
		"https://x/../../../evil", // mkcert accepts as a URI SAN, path escapes the cert dir
		"https://x/../../outside",
		"x/../../outside",
		"../../outside",
		"-install",
		"-uninstall",
		"-cert-file=/tmp/x",
		"unknown.localhost", // valid, but not a configured domain
		"app.localhost.evil",
	} {
		if got := f.get(sni); !bytes.Equal(got, f.defaultLeaf()) {
			t.Errorf("GetCertificate(%q) did not return the default cert", sni)
		}
	}

	if calls := f.mkcertCalls(); len(calls) != 0 {
		t.Errorf("mkcert should not run for untrusted names, got calls %q", calls)
	}
	want := []string{"config/certificates/default.crt", "config/certificates/default.key", "outside.crt", "outside.key", "tpl.crt", "tpl.key"}
	if got := f.certFiles(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("cert files = %q, want %q", got, want)
	}
}

func TestDiskCertProviderMkcert(t *testing.T) {
	f := newCertLookupFixture(t, []string{"app.localhost", "app.example.com"}, "")

	if got := f.get("APP.localhost"); !bytes.Equal(got, f.mkcert) {
		t.Errorf("expected mkcert generated cert for allowed local domain")
	}
	if got := f.get("app.localhost"); !bytes.Equal(got, f.mkcert) {
		t.Errorf("expected mkcert generated cert to be reused")
	}
	certPath := filepath.Join(f.certDir, "app.localhost.crt")
	keyPath := filepath.Join(f.certDir, "app.localhost.key")
	wantCalls := []string{"-cert-file " + certPath + " -key-file " + keyPath + " -- app.localhost"}
	if calls := f.mkcertCalls(); strings.Join(calls, "|") != strings.Join(wantCalls, "|") {
		t.Errorf("mkcert calls = %q, want %q", calls, wantCalls)
	}

	// Allowed, but not a local dev domain: mkcert is not run, cert files on disk are used
	if got := f.get("app.example.com"); !bytes.Equal(got, f.defaultLeaf()) {
		t.Errorf("expected default cert for non local domain without cert files")
	}
	if calls := f.mkcertCalls(); len(calls) != 1 {
		t.Errorf("mkcert should not run for non local domain, got calls %q", calls)
	}
	diskCert := filepath.Join(f.certDir, "app.example.com")
	if err := GenerateSelfSignedCertificate(diskCert+".crt", diskCert+".key", time.Hour); err != nil {
		t.Fatal(err)
	}
	if got := f.get("app.example.com"); !bytes.Equal(got, loadLeaf(t, diskCert+".crt", diskCert+".key")) {
		t.Errorf("expected cert files from disk for allowed domain")
	}

	// Lookup disabled: default cert, existing domain files are ignored
	f.https.EnableCertLookup = false
	if got := f.get("app.localhost"); !bytes.Equal(got, f.defaultLeaf()) {
		t.Errorf("expected default cert with cert lookup disabled")
	}
}

func TestDiskCertProviderMkcertSingleflight(t *testing.T) {
	f := newCertLookupFixture(t, []string{"app.localhost"}, `sleep 0.3; cp "$TPL_CERT" "$2"; cp "$TPL_KEY" "$4"`)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if got := f.get("app.localhost"); !bytes.Equal(got, f.mkcert) {
				t.Errorf("expected mkcert generated cert")
			}
		})
	}
	wg.Wait()
	if calls := f.mkcertCalls(); len(calls) != 1 {
		t.Errorf("expected one mkcert run for concurrent handshakes, got %d", len(calls))
	}
}

func TestDiskCertProviderMkcertFailure(t *testing.T) {
	f := newCertLookupFixture(t, []string{"app.localhost"}, `exit 1`)

	for range 3 {
		if got := f.get("app.localhost"); !bytes.Equal(got, f.defaultLeaf()) {
			t.Errorf("expected default cert when mkcert fails")
		}
	}
	if calls := f.mkcertCalls(); len(calls) != 1 {
		t.Errorf("failed mkcert should not be retried immediately, got %d runs", len(calls))
	}

	// Retried once the failure wait has passed
	f.provider.failureMu.Lock()
	f.provider.mkcertFailures["app.localhost"] = time.Now().Add(-mkcertFailureWait)
	f.provider.failureMu.Unlock()
	f.get("app.localhost")
	if calls := f.mkcertCalls(); len(calls) != 2 {
		t.Errorf("expected mkcert retry after failure wait, got %d runs", len(calls))
	}
}
