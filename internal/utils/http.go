// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"
)

const (
	ApplicationJson = "application/json"
)

type HttpClient struct {
	client    *http.Client
	serverUri string
	user      string
	password  string
}

// NewHttpClient creates a new HttpClient instance
func NewHttpClient(server_uri, user, password string, skipCertCheck bool) *HttpClient {
	customTransport := http.DefaultTransport.(*http.Transport).Clone()
	customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: skipCertCheck}
	client := &http.Client{
		Transport: customTransport,
		Timeout:   time.Duration(180) * time.Second,
	}

	return &HttpClient{
		client:    client,
		serverUri: server_uri,
		user:      user,
		password:  password,
	}
}

func (h *HttpClient) Get(url string, params url.Values, output any) error {
	return h.request(http.MethodGet, url, params, nil, output)
}

func (h *HttpClient) Post(url string, params url.Values, input any, output any) error {
	return h.request(http.MethodPost, url, params, input, output)
}

func (h *HttpClient) Delete(url string, params url.Values) error {
	return h.request(http.MethodDelete, url, params, nil, nil)
}

func (h *HttpClient) request(method, apiPath string, params url.Values, input any, output any) error {
	var resp *http.Response
	var payloadBuf bytes.Buffer

	if input != nil {
		if err := json.NewEncoder(&payloadBuf).Encode(input); err != nil {
			return fmt.Errorf("error encoding request: %w", err)
		}
	}

	u, err := url.Parse(h.serverUri)
	if err != nil {
		return err
	}

	u.Path = path.Join(u.Path, apiPath)
	if params != nil {
		u.RawQuery = params.Encode()
	}
	request, err := http.NewRequest(method, u.String(), &payloadBuf)
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	request.SetBasicAuth(h.user, h.password)
	request.Header.Set("Accept", ApplicationJson)

	if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
		request.Header.Set("Content-Type", ApplicationJson)
	}

	resp, err = h.client.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		errBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		var errResp RequestError
		parseErr := json.Unmarshal(errBody, &errResp)
		if parseErr != nil || errResp.Code == 0 {
			errResp.Code = resp.StatusCode
			errResp.Message = string(errBody)
		}
		return errResp
	}

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if output != nil {
		if err := json.Unmarshal(body, output); err != nil {
			return fmt.Errorf("error parsing response: %w", err)
		}
	}
	return nil
}
