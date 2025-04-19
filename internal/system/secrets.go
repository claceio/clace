// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package system

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/claceio/clace/internal/types"
	"github.com/hashicorp/vault/api"
)

// SecretManager provides access to the secrets for the system
type SecretManager struct {
	// Secrets is a map of secret providers
	providers       map[string]secretProvider
	funcMap         template.FuncMap
	config          map[string]types.SecretConfig
	defaultProvider string
}

func NewSecretManager(ctx context.Context, secretConfig map[string]types.SecretConfig, defaultProvider string) (*SecretManager, error) {
	providers := make(map[string]secretProvider)
	for name, conf := range secretConfig {
		var provider secretProvider
		if name == "asm" || strings.HasPrefix(name, "asm_") {
			provider = &awsSecretProvider{}
		} else if name == "vault" || strings.HasPrefix(name, "vault_") {
			provider = &vaultSecretProvider{}
		} else if name == "env" || strings.HasPrefix(name, "env_") {
			provider = &envSecretProvider{}
		} else if name == "prop" || strings.HasPrefix(name, "prop_") {
			provider = &propertiesSecretProvider{}
		} else {
			return nil, fmt.Errorf("unknown secret provider %s", name)
		}

		err := provider.Configure(ctx, conf)
		if err != nil {
			return nil, err
		}
		providers[name] = provider
	}

	funcMap := GetFuncMap()

	s := &SecretManager{
		providers:       providers,
		funcMap:         funcMap,
		config:          secretConfig,
		defaultProvider: defaultProvider,
	}
	s.funcMap["secret"] = s.templateSecretFunc
	s.funcMap["secret_from"] = s.templateSecretFromFunc
	return s, nil
}

// templateSecretFunc is a template function that retrieves a secret from the default secret manager.
// Since the template function does not support errors, it panics if there is an error
func (s *SecretManager) templateSecretFunc(secretKeys ...string) string {
	return s.appTemplateSecretFunc(nil, s.defaultProvider, "", secretKeys...)
}

// templateSecretFromFunc is a template function that retrieves a secret from the secret manager.
// Since the template function does not support errors, it panics if there is an error
func (s *SecretManager) templateSecretFromFunc(providerName string, secretKeys ...string) string {
	return s.appTemplateSecretFunc(nil, s.defaultProvider, providerName, secretKeys...)
}

// appTemplateSecretFunc is a template function that retrieves a secret from the secret manager.
// Since the template function does not support errors, it panics if there is an error. The appPerms
// are checked to see if the secret can be accessed by the plugin API call
func (s *SecretManager) appTemplateSecretFunc(appPerms [][]string, defaultProvider, providerName string, secretKeys ...string) string {
	if providerName == "" || strings.ToLower(providerName) == "default" {
		// Use the system default provider
		providerName = cmp.Or(defaultProvider, s.defaultProvider)
	}

	provider, ok := s.providers[providerName]
	if !ok {
		panic(fmt.Errorf("unknown secret provider %s", providerName))
	}

	if len(appPerms) == 0 {
		panic("Plugin does not have access to any secrets, update app permissions")
	}

	permMatched := false
	for _, appPerm := range appPerms {
		matched := true
		for i, entry := range secretKeys {
			if i >= len(appPerm) {
				continue
			}
			if appPerm[i] != entry {
				regexMatch, err := types.RegexMatch(appPerm[i], entry)
				if err != nil {
					panic(fmt.Errorf("error matching secret value %s: %w", entry, err))
				}
				if !regexMatch {
					matched = false
					break
				}
			}
		}

		if matched {
			permMatched = true
			break
		}
	}

	if !permMatched {
		panic(fmt.Errorf("plugin does not have access to secret %s", strings.Join(secretKeys, provider.GetJoinDelimiter())))
	}

	secretKey := strings.Join(secretKeys, provider.GetJoinDelimiter())
	config := s.config[providerName]
	printf, ok := config["keys_printf"]
	if ok && len(secretKeys) > 1 {
		printfStr, ok := printf.(string)
		if !ok {
			panic(fmt.Errorf("keys_printf must be a string"))
		}
		args := make([]any, 0, len(secretKeys))
		for _, key := range secretKeys {
			args = append(args, key)
		}
		secretKey = fmt.Sprintf(printfStr, args...)
	}

	ret, err := provider.GetSecret(context.Background(), secretKey)
	if err != nil {
		panic(fmt.Errorf("error getting secret %s from %s: %w", secretKey, providerName, err))
	}
	return ret
}

// EvalTemplate evaluates the input string and replaces any secret placeholders with the actual secret value
func (s *SecretManager) EvalTemplate(input string) (string, error) {
	if len(input) < 4 {
		return input, nil
	}

	if !strings.Contains(input, "{{") || !strings.Contains(input, "}}") {
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

// EvalTemplate evaluates the input string and replaces any secret placeholders with the actual secret value
func (s *SecretManager) AppEvalTemplate(appSecrets [][]string, defaultProvider, input string) (string, error) {
	if len(input) < 4 {
		return input, nil
	}

	if !strings.Contains(input, "{{") || !strings.Contains(input, "}}") {
		return input, nil
	}

	funcMap := template.FuncMap{}
	for name, fn := range s.funcMap {
		funcMap[name] = fn
	}

	secretFunc := func(secretKeys ...string) string {
		return s.appTemplateSecretFunc(appSecrets, defaultProvider, "", secretKeys...)
	}

	secretFromFunc := func(providerName string, secretKeys ...string) string {
		return s.appTemplateSecretFunc(appSecrets, defaultProvider, providerName, secretKeys...)
	}

	funcMap["secret"] = secretFunc
	funcMap["secret_from"] = secretFromFunc

	tmpl, err := template.New("secret template").Funcs(funcMap).Parse(input)
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

	// GetJoinDelimiter returns the delimiter used to join multiple secret keys
	GetJoinDelimiter() string
}

// awsSecretProvider is a secret provider that reads secrets from AWS Secrets Manager
type awsSecretProvider struct {
	client *secretsmanager.Client
}

func (a *awsSecretProvider) Configure(ctx context.Context, conf map[string]any) error {
	profileStr := ""
	profile, ok := conf["profile"]
	if ok {
		profileStr, ok = profile.(string)
		if !ok {
			return fmt.Errorf("profile must be a string")
		}
	}

	var cfg aws.Config
	var err error
	// IAM is automatically supported by config load
	if profileStr != "" {
		cfg, err = config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(profileStr))
	} else {
		cfg, err = config.LoadDefaultConfig(ctx)
	}

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

func (a *awsSecretProvider) GetJoinDelimiter() string {
	return "/"
}

var _ secretProvider = &awsSecretProvider{}

// vaultSecretProvider is a secret provider that reads secrets from HashiCorp Vault
type vaultSecretProvider struct {
	client *api.Client
}

func getConfigString(conf map[string]any, key string) (string, error) {
	value, ok := conf[key]
	if !ok {
		return "", fmt.Errorf("missing '%s' in config", key)
	}

	valueStr, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("'%s' must be a string", key)
	}

	return valueStr, nil
}

func (v *vaultSecretProvider) Configure(ctx context.Context, conf map[string]any) error {
	address, err := getConfigString(conf, "address")
	if err != nil {
		return fmt.Errorf("vault invalid config: %w", err)
	}
	token, err := getConfigString(conf, "token")
	if err != nil {
		return fmt.Errorf("vault invalid config: %w", err)
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

func (v *vaultSecretProvider) GetJoinDelimiter() string {
	return "/"
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

func (e *envSecretProvider) GetJoinDelimiter() string {
	return "_"
}

var _ secretProvider = &envSecretProvider{}
