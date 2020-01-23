// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.
package v1alpha1

import (
	capzv1alpha2 "sigs.k8s.io/cluster-api-provider-azure/api/v1alpha2"
)

// APIEndpoint represents a reachable Kubernetes API endpoint.
type APIEndpoint struct {
	// The hostname on which the API server is serving.
	Host string `json:"host"`
	// The port on which the API server is serving.
	Port int `json:"port"`
}

type AzureMachinePoolSpec struct {
	Name     string                        `json:"name"`
	Template capzv1alpha2.AzureMachineSpec `json:"template"`
}
