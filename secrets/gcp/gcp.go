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

// Package gcp provides a Google Secret Manager backed
// [github.com/mikehelmick/go-bananas/secrets.SecretManager].
//
// Blank-import this package to register the "GOOGLE_SECRET_MANAGER" provider,
// then select it via [github.com/mikehelmick/go-bananas/secrets.Config.Type]:
//
//	import _ "github.com/mikehelmick/go-bananas/secrets/gcp"
//
// Because the Google Cloud SDK is only imported here, binaries that do not use
// this provider never compile or link it.
package gcp

import (
	"context"
	"fmt"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/mikehelmick/go-bananas/secrets"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func init() {
	secrets.RegisterManager("GOOGLE_SECRET_MANAGER", NewGoogleSecretManager)
}

var _ secrets.SecretVersionManager = (*GoogleSecretManager)(nil)

// GoogleSecretManager is a [github.com/mikehelmick/go-bananas/secrets.SecretManager]
// backed by Google Secret Manager.
type GoogleSecretManager struct {
	client *secretmanager.Client
}

// NewGoogleSecretManager creates a new Google Secret Manager client using
// Application Default Credentials.
func NewGoogleSecretManager(ctx context.Context, _ *secrets.Config) (secrets.SecretManager, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("secretmanager.NewClient: %w", err)
	}
	return &GoogleSecretManager{client: client}, nil
}

// GetSecretValue accesses a secret version. Names are of the form
// projects/PROJECT/secrets/NAME/versions/VERSION.
func (sm *GoogleSecretManager) GetSecretValue(ctx context.Context, name string) (string, error) {
	result, err := sm.client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: name,
	})
	if err != nil {
		return "", fmt.Errorf("failed to access secret %v: %w", name, err)
	}
	return string(result.Payload.Data), nil
}

// CreateSecretVersion adds a new version to the parent secret and returns its
// resource name.
func (sm *GoogleSecretManager) CreateSecretVersion(ctx context.Context, parent string, data []byte) (string, error) {
	version, err := sm.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent: parent,
		Payload: &secretmanagerpb.SecretPayload{
			Data: data,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create secret version: %w", err)
	}
	return version.GetName(), nil
}

// DestroySecretVersion destroys the named secret version. A missing version is
// not an error.
func (sm *GoogleSecretManager) DestroySecretVersion(ctx context.Context, name string) error {
	if _, err := sm.client.DestroySecretVersion(ctx, &secretmanagerpb.DestroySecretVersionRequest{
		Name: name,
	}); err != nil {
		if grpcstatus.Code(err) == grpccodes.NotFound {
			return nil
		}
		return fmt.Errorf("failed to destroy secret version: %w", err)
	}
	return nil
}
