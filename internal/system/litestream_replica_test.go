// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package system

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/openrundev/openrun/internal/types"
)

func TestRecordReplicaObject(t *testing.T) {
	t.Parallel()

	now := time.Now()
	found := map[string]*LitestreamReplicaDB{}

	// Observed litestream 0.5 remote layout: <db>/<0000-level>/<min>-<max>.ltx
	recordReplicaObject(found, "data.db/0000/0000000000000001-0000000000000003.ltx", now.Add(-time.Minute), 100)
	recordReplicaObject(found, "data.db/0009/0000000000000001-0000000000000002.ltx", now, 500)
	// Nested database path
	recordReplicaObject(found, "tenants/acme.db/0000/0000000000000001-0000000000000001.ltx", now, 50)
	// Alternate layout with an ltx path segment
	recordReplicaObject(found, "extra.db/ltx/0/0000000000000001-000000000000000a.ltx", now, 70)
	// Non-LTX objects are ignored
	recordReplicaObject(found, "data.db/.litestream/position", now, 10)
	recordReplicaObject(found, "stray.txt", now, 10)

	if len(found) != 3 {
		t.Fatalf("found = %v, want 3 databases", found)
	}
	dataDB := found["data.db"]
	if dataDB == nil || dataDB.MaxTXID != 3 || dataDB.Size != 600 {
		t.Fatalf("data.db = %+v", dataDB)
	}
	if !dataDB.LastUpdated.Equal(now) {
		t.Fatalf("data.db last updated = %v, want %v", dataDB.LastUpdated, now)
	}
	if found["tenants/acme.db"] == nil {
		t.Fatalf("nested db missing: %v", found)
	}
	if extra := found["extra.db"]; extra == nil || extra.MaxTXID != 10 {
		t.Fatalf("extra.db = %+v", found["extra.db"])
	}
}

func TestLitestreamReplicaURL(t *testing.T) {
	t.Parallel()

	s3URL, err := LitestreamReplicaURL(types.LitestreamConfig{
		Bucket: "b", Endpoint: "http://127.0.0.1:9000", Region: "us-east-1", ForcePathStyle: true,
	}, "prefix/bindings/bnd_1/prod/data.db")
	if err != nil {
		t.Fatalf("LitestreamReplicaURL: %v", err)
	}
	want := "s3://b/prefix/bindings/bnd_1/prod/data.db?endpoint=http%3A%2F%2F127.0.0.1%3A9000&forcePathStyle=true&region=us-east-1"
	if s3URL != want {
		t.Fatalf("url = %q, want %q", s3URL, want)
	}

	off := false
	noSignURL, err := LitestreamReplicaURL(types.LitestreamConfig{
		Bucket: "b", Endpoint: "https://garage.example.com", SignAcceptEncoding: &off,
	}, "metadata/meta.db")
	if err != nil {
		t.Fatalf("LitestreamReplicaURL sign_accept_encoding: %v", err)
	}
	want = "s3://b/metadata/meta.db?endpoint=https%3A%2F%2Fgarage.example.com&signAcceptEncoding=false"
	if noSignURL != want {
		t.Fatalf("url = %q, want %q", noSignURL, want)
	}

	fileURL, err := LitestreamReplicaURL(types.LitestreamConfig{Type: "file", Path: "/backups"}, "metadata/meta.db")
	if err != nil {
		t.Fatalf("LitestreamReplicaURL file: %v", err)
	}
	if fileURL != "file:///backups/metadata/meta.db" {
		t.Fatalf("file url = %q", fileURL)
	}
}

// hostRewriteTransport sends every request to target regardless of the URL's
// host, so a client configured for a real endpoint host (GCS) can be tested
// against a local fake. Only the wire destination changes: the request URL,
// Host header and signature are the ones the client computed for the
// configured endpoint.
type hostRewriteTransport struct{ target *url.URL }

func (t hostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = t.target.Scheme
	req.URL.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

// TestLitestreamS3ClientSignedHeaders verifies the replica listing client
// signs requests the way litestream's own s3 client does: Accept-Encoding is
// left out of the signature for Google Cloud Storage endpoints and when
// sign_accept_encoding is false (the header is rewritten in transit by those
// services, which then reject the signature), and signed otherwise.
func TestLitestreamS3ClientSignedHeaders(t *testing.T) {
	t.Parallel()

	off, on := false, true
	tests := []struct {
		name       string
		config     types.LitestreamConfig
		wantSigned bool
	}{
		{"gcs endpoint", types.LitestreamConfig{Endpoint: "https://storage.googleapis.com", ForcePathStyle: true}, false},
		{"gcs endpoint explicit true", types.LitestreamConfig{Endpoint: "https://storage.googleapis.com", SignAcceptEncoding: &on}, false},
		{"custom endpoint default", types.LitestreamConfig{Endpoint: "https://s3.example.com", ForcePathStyle: true}, true},
		{"custom endpoint sign off", types.LitestreamConfig{Endpoint: "https://s3.example.com", SignAcceptEncoding: &off}, false},
		{"aws default", types.LitestreamConfig{Region: "us-west-2"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/xml")
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` +
					`<ListBucketResult><Name>b</Name><KeyCount>0</KeyCount><IsTruncated>false</IsTruncated></ListBucketResult>`))
			}))
			defer srv.Close()
			target, _ := url.Parse(srv.URL)

			cfg := tc.config
			cfg.Bucket = "b"
			cfg.AccessKeyId = "AKID"
			cfg.SecretAccessKey = "SECRET"
			client, err := newLitestreamS3Client(context.Background(), cfg, func(o *awss3.Options) {
				o.HTTPClient = &http.Client{Transport: hostRewriteTransport{target: target}}
			})
			if err != nil {
				t.Fatalf("newLitestreamS3Client: %v", err)
			}
			if _, err := client.ListObjectsV2(context.Background(), &awss3.ListObjectsV2Input{Bucket: &cfg.Bucket}); err != nil {
				t.Fatalf("ListObjectsV2: %v", err)
			}

			if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 ") {
				t.Fatalf("request not sigv4 signed: %q", gotAuth)
			}
			_, signedHeaders, ok := strings.Cut(gotAuth, "SignedHeaders=")
			if !ok {
				t.Fatalf("no SignedHeaders in %q", gotAuth)
			}
			signedHeaders, _, _ = strings.Cut(signedHeaders, ",")
			gotSigned := false
			for _, h := range strings.Split(signedHeaders, ";") {
				if h == "accept-encoding" {
					gotSigned = true
				}
			}
			if gotSigned != tc.wantSigned {
				t.Fatalf("accept-encoding signed = %v, want %v (SignedHeaders=%s)", gotSigned, tc.wantSigned, signedHeaders)
			}
		})
	}
}
