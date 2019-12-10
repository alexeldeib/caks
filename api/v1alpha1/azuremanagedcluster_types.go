// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AzureManagedClusterSpec defines the desired state of AzureManagedCluster
type AzureManagedClusterSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of AzureManagedCluster. Edit AzureManagedCluster_types.go to remove/update
	Foo string `json:"foo,omitempty"`
}

// AzureManagedClusterStatus defines the observed state of AzureManagedCluster
type AzureManagedClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true

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
