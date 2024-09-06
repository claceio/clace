// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package system

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/claceio/clace/internal/types"
	"github.com/hashicorp/vault/api"
)

// SecretManager provides access to the secrets for the system
type SecretManager struct {
	// Secrets is a map of secret providers
	providers map[string]secretProvider
	funcMap   template.FuncMap
}

func NewSecretManager(ctx context.Context, secretConfig map[string]types.SecretConfig) (*SecretManager, error) {
	providers := make(map[string]secretProvider)
	for name, conf := range secretConfig {
		var provider secretProvider
		if name == "asm" || strings.HasPrefix(name, "asm_") {
			provider = &awsSecretProvider{}
		} else if name == "vault" || strings.HasPrefix(name, "vault_") {
			provider = &vaultSecretProvider{}
		} else if name == "env" {
			provider = &envSecretProvider{}
		} else {
			return nil, fmt.Errorf("unknown secret provider %s", name)
		}

		err := provider.Configure(ctx, conf)
		if err != nil {
			return nil, err
		}
		providers[name] = provider
	}

	funcMap := sprig.FuncMap()
	delete(funcMap, "env")
	delete(funcMap, "expandenv")

	s := &SecretManager{
		providers: providers,
		funcMap:   funcMap,
	}
	s.funcMap["secret"] = s.templateSecretFunc
	return s, nil
}

// templateSecretFunc is a template function that retrieves a secret from the secret manager.
// Since the template function does not support errors, it panics if there is an error
func (s *SecretManager) templateSecretFunc(providerName, secretName string) string {
	provider, ok := s.providers[providerName]
	if !ok {
		panic(fmt.Errorf("unknown secret provider %s", providerName))
	}

	ret, err := provider.GetSecret(context.Background(), secretName)
	if err != nil {
		panic(fmt.Errorf("error getting secret %s from %s: %w", secretName, providerName, err))
	}
	return ret
}

// EvalTemplate evaluates the input string and replaces any secret placeholders with the actual secret value
func (s *SecretManager) EvalTemplate(input string) (string, error) {
	if len(input) < 4 {
		return input, nil
	}
	if input[0] != '{' || input[1] != '{' || input[len(input)-1] != '}' || input[len(input)-2] != '}' {
		return input, nil
	}

	tmpl, err := template.New("secret template").Funcs(s.funcMap).Parse(input)
	if err != nil {
		return "", err
	}
	var doc bytes.Buffer
	err = tmpl.Execute(&doc, nil)
	if err != nil {
		return "", err
	}
	return doc.String(), nil
}

// secretProvider is an interface for secret providers
type secretProvider interface {
	// Configure is called to configure the secret provider
	Configure(ctx context.Context, conf map[string]any) error

	// GetSecret returns the secret value for the given secret name
	GetSecret(ctx context.Context, secretName string) (string, error)
}

// awsSecretProvider is a secret provider that reads secrets from AWS Secrets Manager
type awsSecretProvider struct {
	client *secretsmanager.Client
}

func (a *awsSecretProvider) Configure(ctx context.Context, conf map[string]any) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	// IAM is automatically supported by default config
	if err != nil {
		return err
	}

	a.client = secretsmanager.NewFromConfig(cfg)
	return nil
}

func (a *awsSecretProvider) GetSecret(ctx context.Context, secretName string) (string, error) {
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	}
	result, err := a.client.GetSecretValue(ctx, input)
	if err != nil {
		return "", err
	}
	return aws.ToString(result.SecretString), nil
}

var _ secretProvider = &awsSecretProvider{}

// vaultSecretProvider is a secret provider that reads secrets from HashiCorp Vault
type vaultSecretProvider struct {
	client *api.Client
}

func getConfigString(conf map[string]any, key string) (string, error) {
	value, ok := conf[key]
	if !ok {
		return "", fmt.Errorf("missing %s in config", key)
	}

	valueStr, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}

	return valueStr, nil
}

func (v *vaultSecretProvider) Configure(ctx context.Context, conf map[string]any) error {
	address, err := getConfigString(conf, "address")
	if err != nil {
		return err
	}
	token, err := getConfigString(conf, "token")
	if err != nil {
		return err
	}

	vaultConfig := &api.Config{
		Address: address,
	}

	client, err := api.NewClient(vaultConfig)
	if err != nil {
		return err
	}

	// Set the token for authentication
	client.SetToken(token)
	v.client = client
	return nil
}

func (v *vaultSecretProvider) GetSecret(ctx context.Context, secretName string) (string, error) {
	secret, err := v.client.Logical().Read(secretName)
	if err != nil {
		return "", err
	}

	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("key %s not found", secretName)
	}

	value, ok := secret.Data[secretName].(string)
	if !ok {
		return "", fmt.Errorf("key %s must be string", secretName)
	}

	return value, nil
}

var _ secretProvider = &vaultSecretProvider{}

// envSecretProvider is a secret provider that reads secrets from environment variables
type envSecretProvider struct {
}

func (e *envSecretProvider) Configure(ctx context.Context, conf map[string]any) error {
	return nil
}

func (e *envSecretProvider) GetSecret(ctx context.Context, secretName string) (string, error) {
	return os.Getenv(secretName), nil
}

var _ secretProvider = &envSecretProvider{}
