// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.
package services

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-03-01/compute"
	infrav1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
	"github.com/Azure/go-autorest/autorest"
)

// type VMSSService interface {
// 	Get(ctx context.Context, cluster *infrav1.AzureManagedCluster) (compute.VirtualMachineScaleSet, error)
//  List(ctx context.Context, cluster *infrav1.AzureManagedCluster) (compute.VirtualMachineScaleSetListResultIterator, error)
// }

// VMSSService ...
type VMSSService struct {
	authorizer autorest.Authorizer
}

// NewVMSSService ...
func NewVMSSService(authorizer autorest.Authorizer) *VMSSService {
	return &VMSSService{
		authorizer,
	}
}

func newVMSSClient(subscriptionID string, authorizer autorest.Authorizer) (compute.VirtualMachineScaleSetsClient, error) {
	client := compute.NewVirtualMachineScaleSetsClient(subscriptionID)
	if err := client.AddToUserAgent("cluster-api-provider-aks"); err != nil {
		return compute.VirtualMachineScaleSetsClient{}, err
	}
	client.Authorizer = authorizer
	return client, nil
}

// Get ...
func (svc *VMSSService) Get(ctx context.Context, cluster *infrav1.AzureManagedCluster) (compute.VirtualMachineScaleSet, error) {
	client, err := newVMSSClient(cluster.Spec.SubscriptionID, svc.authorizer)
	if err != nil {
		return compute.VirtualMachineScaleSet{}, err
	}
	return client.Get(ctx, cluster.Spec.ResourceGroup, cluster.Spec.Name)
}

// List ...
func (svc *VMSSService) List(ctx context.Context, cluster *infrav1.AzureManagedCluster) (compute.VirtualMachineScaleSetListResultIterator, error) {
	client, err := newVMSSClient(cluster.Spec.SubscriptionID, svc.authorizer)
	if err != nil {
		return compute.VirtualMachineScaleSetListResultIterator{}, err
	}
	return client.ListComplete(ctx, cluster.Spec.ResourceGroup)
	// iterator, err := client.ListComplete(ctx, cluster.Spec.ResourceGroup)
	// if err != nil {
	// 	return compute.VirtualMachineScaleSetListResultIterator{}, err
	// }
	// list := []compute.VirtualMachineScaleSet{}
	// for iterator.NotDone() {
	// 	vmss := iterator.Value()
	// 	list = append(list, vmss)
	// 	if err := iterator.NextWithContext(ctx); err != nil {
	// 		return compute.VirtualMachineScaleSetListResultIterator{}, err
	// 	}
	// }
	// return list, nil
}
