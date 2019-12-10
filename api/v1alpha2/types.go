// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.
package v1alpha2

// APIEndpoint represents a reachable Kubernetes API endpoint.
type APIEndpoint struct {
	// The hostname on which the API server is serving.
	Host string `json:"host"`
	// The port on which the API server is serving.
	Port int `json:"port"`
}
