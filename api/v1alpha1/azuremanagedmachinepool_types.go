// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capzv1alpha2 "sigs.k8s.io/cluster-api-provider-azure/api/v1alpha2"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AzureManagedMachinePoolSpec defines the desired state of AzureManagedMachinePool
type AzureManagedMachinePoolSpec struct {
	Name     string                        `json:"name"`
	Template capzv1alpha2.AzureMachineSpec `json:"template"`
}

// AzureManagedMachinePoolStatus defines the observed state of AzureManagedMachinePool
type AzureManagedMachinePoolStatus struct {
	// Replicas is the most recently observed number of replicas.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// Ready is true when the machine pool infrastructure is ready to be useds.
	Ready       bool `json:"ready"`
}

// +kubebuilder:object:root=true

// AzureManagedMachinePool is the Schema for the azuremanagedmachinepools API
type AzureManagedMachinePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AzureManagedMachinePoolSpec   `json:"spec,omitempty"`
	Status AzureManagedMachinePoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AzureManagedMachinePoolList contains a list of AzureManagedMachinePool
type AzureManagedMachinePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AzureManagedMachinePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AzureManagedMachinePool{}, &AzureManagedMachinePoolList{})
}
