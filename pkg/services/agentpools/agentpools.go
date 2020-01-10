// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.
package agentpools

import (
	"context"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2019-11-01/containerservice"
	infrav1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
	"github.com/Azure/go-autorest/autorest"
)

// // AgentPoolService is
// type AgentPoolSeviceInterface interface {
// 	Get(ctx context.Context, cluster *infrav1.AzureManagedCluster, machine *infrav1.AzureManagedMachine) (containerservice.AgentPool, error)
// 	CreateOrUpdate(ctx context.Context, cluster *infrav1.AzureManagedCluster, agentPool containerservice.AgentPool) (err error)
// 	Delete(ctx context.Context, cluster *infrav1.AzureManagedCluster, agentPool containerservice.AgentPool) (done bool, err error)
// }

// AgentPoolService abstracts operations with the Azure Kubernetes Agent Pool APIs
type AgentPoolService struct {
	authorizer autorest.Authorizer
}

// NewAgentPoolService provides a new AgentPool service
func NewAgentPoolService(authorizer autorest.Authorizer) *AgentPoolService {
	return &AgentPoolService{
		authorizer,
	}
}

func newAgentPoolsClient(subscriptionID string, authorizer autorest.Authorizer) (containerservice.AgentPoolsClient, error) {
	client := containerservice.NewAgentPoolsClient(subscriptionID)
	if err := client.AddToUserAgent("cluster-api-provider-aks"); err != nil {
		return containerservice.AgentPoolsClient{}, err
	}
	client.Authorizer = authorizer
	return client, nil
}

// Get fetches
func (svc *AgentPoolService) Get(ctx context.Context, cluster *infrav1.AzureManagedCluster, machine *infrav1.AzureManagedMachine) (containerservice.AgentPool, error) {
	client, err := newAgentPoolsClient(cluster.Spec.SubscriptionID, svc.authorizer)
	if err != nil {
		return containerservice.AgentPool{}, err
	}
	return client.Get(ctx, cluster.Spec.ResourceGroup, cluster.Spec.Name, machine.Spec.Pool)
}

// CreateOrUpdate applies
func (svc *AgentPoolService) CreateOrUpdate(ctx context.Context, cluster *infrav1.AzureManagedCluster, agentPool containerservice.AgentPool) (err error) {
	client, err := newAgentPoolsClient(cluster.Spec.SubscriptionID, svc.authorizer)
	if err != nil {
		return err
	}
	_, err = client.CreateOrUpdate(ctx, cluster.Spec.ResourceGroup, cluster.Spec.Name, *agentPool.Name, agentPool)
	return err
}

// Delete deletes
func (svc *AgentPoolService) Delete(ctx context.Context, cluster *infrav1.AzureManagedCluster, agentPool containerservice.AgentPool) (done bool, err error) {
	client, err := newAgentPoolsClient(cluster.Spec.SubscriptionID, svc.authorizer)
	if err != nil {
		return false, err
	}
	future, err := client.Delete(ctx, cluster.Spec.ResourceGroup, cluster.Spec.Name, *agentPool.Name)
	if err != nil {
		if res := future.Response(); res != nil && res.StatusCode == http.StatusNotFound {
			return true, nil
		}
		return false, err
	}
	return false, nil
}
