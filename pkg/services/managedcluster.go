package services

// import (
// 	"context"

// 	"github.com/Azure/azure-sdk-for-go/profiles/latest/containerservice/mgmt/containerservice"
// 	infrav1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
// 	"github.com/Azure/go-autorest/autorest"
// 	"github.com/Azure/go-autorest/autorest/azure"
// 	"github.com/Azure/go-autorest/autorest/azure/auth"
// )

// type ManagedClusterService struct {
// 	authorizer autorest.Authorizer
// }

// func NewManagedClusterService() (*ManagedClusterService, error) {
// 	authorizer, err := auth.NewAuthorizerFromFile(azure.PublicCloud.ResourceManagerEndpoint)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return &ManagedClusterService{
// 		authorizer,
// 	}, nil
// }

// func newManagedClustersClient(subscriptionID string, authorizer autorest.Authorizer) (containerservice.ManagedClustersClient, error) {
// 	client := containerservice.NewManagedClustersClient(subscriptionID)
// 	if err := client.AddToUserAgent("cluster-api-provider-aks"); err != nil {
// 		return containerservice.ManagedClustersClient{}, err
// 	}
// 	client.Authorizer = authorizer
// 	return client, nil
// }

// func (svc *ManagedClusterService) CreateOrUpdate(ctx context.Context, local *infrav1.AzureManagedCluster) (done bool, err error) {
// 	client, err := newManagedClustersClient(local.Spec.SubscriptionID, svc.authorizer)
// 	if err != nil {
// 		return false, err
// 	}

// }
