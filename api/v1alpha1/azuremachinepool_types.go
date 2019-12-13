// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AzureMachinePoolSpec defines the desired state of AzureMachinePool
type AzureMachinePoolSpec struct {
	// Name of the node pool.
	Name string `json:"name"`
	// SKU of the VMs in the node pool.
	SKU string `json:"sku"`
	// Capacity is the number of VMs in a node pool.
	// Capacity int32 `json:"capacity"`
}

// AzureMachinePoolStatus defines the observed state of AzureMachinePool
type AzureMachinePoolStatus struct {
}

// +kubebuilder:object:root=true

// AzureMachinePool is the Schema for the azuremachinepools API
type AzureMachinePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AzureMachinePoolSpec   `json:"spec,omitempty"`
	Status AzureMachinePoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AzureMachinePoolList contains a list of AzureMachinePool
type AzureMachinePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AzureMachinePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AzureMachinePool{}, &AzureMachinePoolList{})
}
