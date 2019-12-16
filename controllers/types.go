package controllers

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2019-10-01/containerservice"
	infrav1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
)

// ManagedClusterService ...
type ManagedClusterService interface {
	Get(ctx context.Context, cluster *infrav1.AzureManagedCluster) (containerservice.ManagedCluster, error)
	CreateOrUpdate(ctx context.Context, cluster *infrav1.AzureManagedCluster, azureCluster containerservice.ManagedCluster) (err error)
	Delete(ctx context.Context, cluster *infrav1.AzureManagedCluster) (done bool, err error)
	GetCredentials(ctx context.Context, cluster *infrav1.AzureManagedCluster) ([]byte, error)
}

// AgentPoolService ...
type AgentPoolService interface {
	Get(ctx context.Context, cluster *infrav1.AzureManagedCluster, machine *infrav1.AzureManagedMachine) (containerservice.AgentPool, error)
	CreateOrUpdate(ctx context.Context, cluster *infrav1.AzureManagedCluster, agentPool containerservice.AgentPool) (err error)
	Delete(ctx context.Context, cluster *infrav1.AzureManagedCluster, agentPool containerservice.AgentPool) (done bool, err error)
}

// VMSSService ...
type VMSSService interface {
	Get(ctx context.Context, cluster *infrav1.AzureManagedCluster) (compute.VirtualMachineScaleSet, error)
	List(ctx context.Context, cluster *infrav1.AzureManagedCluster) (compute.VirtualMachineScaleSetListResultIterator, error)
}

// VMSSInstanceService ...
type VMSSInstanceService interface {
	Get(ctx context.Context, machine *infrav1.AzureManagedMachine) (compute.VirtualMachineScaleSetVM, error)
	Delete(ctx context.Context, machine *infrav1.AzureManagedMachine) (done bool, err error)
}
