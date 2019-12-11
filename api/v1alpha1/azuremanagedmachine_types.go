// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AzureManagedMachineSpec defines the desired state of AzureManagedMachine
type AzureManagedMachineSpec struct {
}

// AzureManagedMachineStatus defines the observed state of AzureManagedMachine
type AzureManagedMachineStatus struct {
}

// +kubebuilder:object:root=true

// AzureManagedMachine is the Schema for the azuremanagedmachines API
type AzureManagedMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AzureManagedMachineSpec   `json:"spec,omitempty"`
	Status AzureManagedMachineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AzureManagedMachineList contains a list of AzureManagedMachine
type AzureManagedMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AzureManagedMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AzureManagedMachine{}, &AzureManagedMachineList{})
}
