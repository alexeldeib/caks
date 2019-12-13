// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AzureManagedClusterSpec defines the desired state of AzureManagedCluster
type AzureManagedClusterSpec struct {
	// SubscriptionID is the subscription id for an azure resource.
	// +kubebuilder:validation:Pattern=`^[0-9A-Fa-f]{8}(?:-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12}$`
	SubscriptionID string `json:"subscriptionId"`
	// ResourceGroup is the resource group name for an azure resource.
	// +kubebuilder:validation:Pattern=`^[-\w\._\(\)]+$`
	ResourceGroup string `json:"resourceGroup"`
	// Location is the region where the azure resource resides.
	Location string `json:"location"`
	// Name is the name of the managed cluster in Azure.
	Name string `json:"name"`
	// SSHPublicKey is a string literal containing an ssh public key.
	SSHPublicKey string `json:"sshPublicKey"`
	// Version defines the kubernetes version of the cluster.
	Version string `json:"version"`
	// NodePools is the list of additional node pools managed by this cluster.
	// +kubebuilder:validation:MinItems=1
	NodePools []AzureMachinePoolSpec `json:"nodePools"`
}

// AzureManagedClusterStatus defines the observed state of AzureManagedCluster
type AzureManagedClusterStatus struct {
	// Ready is true when the cluster infrastructure is ready for dependent steps to utilize it.
	Ready           bool    `json:"ready"`
	DefaultNodePool *string `json:"defaultNodePool,omitempty"`
	// APIEndpoints represents the endpoints to communicate with the control plane.
	// +optional
	APIEndpoints []APIEndpoint `json:"apiEndpoints,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AzureManagedCluster is the Schema for the azuremanagedclusters API
type AzureManagedCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AzureManagedClusterSpec   `json:"spec,omitempty"`
	Status AzureManagedClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AzureManagedClusterList contains a list of AzureManagedCluster
type AzureManagedClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AzureManagedCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AzureManagedCluster{}, &AzureManagedClusterList{})
}
