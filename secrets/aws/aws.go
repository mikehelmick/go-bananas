// Copyright 2026 the go-bananas authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package aws provides an AWS Secrets Manager backed
// [github.com/mikehelmick/go-bananas/secrets.SecretManager].
//
// Blank-import this package to register the "AWS_SECRETS_MANAGER" provider:
//
//	import _ "github.com/mikehelmick/go-bananas/secrets/aws"
//
// The AWS SDK (v2) is imported only here, so binaries that do not use this
// provider never compile or link it. Configuration is read from the standard AWS
// environment and credential chain.
package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/mikehelmick/go-bananas/secrets"
)

func init() {
	secrets.RegisterManager("AWS_SECRETS_MANAGER", NewSecretsManager)
}

var _ secrets.SecretManager = (*SecretsManager)(nil)

// SecretsManager is a [github.com/mikehelmick/go-bananas/secrets.SecretManager]
// backed by AWS Secrets Manager.
type SecretsManager struct {
	client *secretsmanager.Client
}

// NewSecretsManager creates a new AWS Secrets Manager client using the
// default AWS configuration (environment, shared config, IAM role, etc.).
func NewSecretsManager(ctx context.Context, _ *secrets.Config) (secrets.SecretManager, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return &SecretsManager{client: secretsmanager.NewFromConfig(cfg)}, nil
}

// GetSecretValue reads a secret. The name may include an optional version and
// stage: "SECRET@VERSION#STAGE", where SECRET is the name or ARN, VERSION is the
// version id, and STAGE is one of AWSCURRENT or AWSPREVIOUS. Values are returned
// as plaintext strings.
func (sm *SecretsManager) GetSecretValue(ctx context.Context, name string) (string, error) {
	var secretID, versionID, versionStage string

	current := &secretID
	for _, ch := range name {
		switch ch {
		case '@':
			current = &versionID
		case '#':
			current = &versionStage
		default:
			*current += string(ch)
		}
	}

	req := &secretsmanager.GetSecretValueInput{SecretId: aws.String(secretID)}
	if versionID != "" {
		req.VersionId = aws.String(versionID)
	}
	if versionStage != "" {
		req.VersionStage = aws.String(versionStage)
	}

	result, err := sm.client.GetSecretValue(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to access secret %v: %w", name, err)
	}

	if v := aws.ToString(result.SecretString); v != "" {
		return v, nil
	}
	return string(result.SecretBinary), nil
}
