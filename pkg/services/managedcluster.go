package services

import (
	"context"
	"errors"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2019-10-01/containerservice"
	infrav1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
	"github.com/Azure/go-autorest/autorest"
)

// type ManagedClusterService interface {
// 	Get(ctx context.Context, cluster *infrav1.AzureManagedCluster) (containerservice.ManagedCluster, error)
// 	CreateOrUpdate(ctx context.Context, cluster *infrav1.AzureManagedCluster, azureCluster containerservice.ManagedCluster) (err error)
// 	Delete(ctx context.Context, cluster *infrav1.AzureManagedCluster) (done bool, err error)
// }

// ManagedClusterService ...
type ManagedClusterService struct {
	authorizer autorest.Authorizer
}

// NewManagedClusterService ...
func NewManagedClusterService(authorizer autorest.Authorizer) *ManagedClusterService {
	return &ManagedClusterService{
		authorizer,
	}
}

func newManagedClustersClient(subscriptionID string, authorizer autorest.Authorizer) (containerservice.ManagedClustersClient, error) {
	client := containerservice.NewManagedClustersClient(subscriptionID)
	if err := client.AddToUserAgent("cluster-api-provider-aks"); err != nil {
		return containerservice.ManagedClustersClient{}, err
	}
	client.Authorizer = authorizer
	return client, nil
}

// Get ...
func (svc *ManagedClusterService) Get(ctx context.Context, cluster *infrav1.AzureManagedCluster) (containerservice.ManagedCluster, error) {
	client, err := newManagedClustersClient(cluster.Spec.SubscriptionID, svc.authorizer)
	if err != nil {
		return containerservice.ManagedCluster{}, err
	}
	return client.Get(ctx, cluster.Spec.ResourceGroup, cluster.Spec.Name)
}

// CreateOrUpdate ...
func (svc *ManagedClusterService) CreateOrUpdate(ctx context.Context, cluster *infrav1.AzureManagedCluster, azureCluster containerservice.ManagedCluster) (err error) {
	client, err := newManagedClustersClient(cluster.Spec.SubscriptionID, svc.authorizer)
	if err != nil {
		return err
	}
	_, err = client.CreateOrUpdate(ctx, cluster.Spec.ResourceGroup, cluster.Spec.Name, azureCluster)
	return err
}

// Delete ...
func (svc *ManagedClusterService) Delete(ctx context.Context, cluster *infrav1.AzureManagedCluster) (done bool, err error) {
	client, err := newManagedClustersClient(cluster.Spec.SubscriptionID, svc.authorizer)
	if err != nil {
		return false, err
	}
	future, err := client.Delete(ctx, cluster.Spec.ResourceGroup, cluster.Spec.Name)
	if err != nil {
		if res := future.Response(); res != nil && res.StatusCode == http.StatusNotFound {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// GetCredentials ...
func (svc *ManagedClusterService) GetCredentials(ctx context.Context, cluster *infrav1.AzureManagedCluster) ([]byte, error) {
	client, err := newManagedClustersClient(cluster.Spec.SubscriptionID, svc.authorizer)
	if err != nil {
		return nil, err
	}

	credentialList, err := client.ListClusterAdminCredentials(ctx, cluster.Spec.ResourceGroup, cluster.Spec.Name)
	if err != nil {
		return nil, err
	}

	if credentialList.Kubeconfigs == nil || len(*credentialList.Kubeconfigs) < 1 {
		return nil, errors.New("no kubeconfigs available for the aks cluster")
	}

	return *(*credentialList.Kubeconfigs)[0].Value, nil
}
