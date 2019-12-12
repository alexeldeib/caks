// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AzureManagedMachineSpec defines the desired state of AzureManagedMachine
type AzureManagedMachineSpec struct {
	// Pool is the name of the agent pool this machine is part of.
	// Defaults to primary node pool.
	// +optional
	Pool *string `json:"pool,omitempty"`
	// ProviderID is the unique identifier as specified by the cloud provider.
	ProviderID *string `json:"providerID,omitempty"`
}

// AzureManagedMachineStatus defines the observed state of AzureManagedMachine
type AzureManagedMachineStatus struct {
	// Ready is true when the provider resource is ready.
	Ready bool `json:"ready"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AzureManagedMachine is the Schema for the azuremanagedmachines API
type AzureManagedMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AzureManagedMachineSpec   `json:"spec,omitempty"`
	Status AzureManagedMachineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AzureManagedMachineList contains a list of AzureManagedMachine
type AzureManagedMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AzureManagedMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AzureManagedMachine{}, &AzureManagedMachineList{})
}
