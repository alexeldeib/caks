package managedclusters

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2019-11-01/containerservice"
	"github.com/Azure/go-autorest/autorest"

	"github.com/Azure/cluster-api-provider-aks/pkg/constants"
)

type client struct {
	containerservice.ManagedClustersClient
}

func newClient(authorizer autorest.Authorizer, subscriptionID string) (*client, error) {
	var c = containerservice.NewManagedClustersClient(subscriptionID)
	c.Authorizer = authorizer
	if err := c.AddToUserAgent(constants.UserAgent); err != nil {
		return nil, err
	}
	return &client{c}, nil
}

func (c *client) createOrUpdate(ctx context.Context, group, name string, properties containerservice.ManagedCluster) (containerservice.ManagedCluster, error) {
	future, err := c.ManagedClustersClient.CreateOrUpdate(ctx, group, name, properties)
	if err != nil {
		return containerservice.ManagedCluster{}, err
	}
	if err := future.WaitForCompletionRef(ctx, c.Client); err != nil {
		return containerservice.ManagedCluster{}, err
	}
	return future.Result(c.ManagedClustersClient)
}

func (c *client) get(ctx context.Context, group, name string) (containerservice.ManagedCluster, error) {
	return c.ManagedClustersClient.Get(ctx, group, name)
}

func (c *client) delete(ctx context.Context, group, name string) error {
	future, err := c.ManagedClustersClient.Delete(ctx, group, name)
	if err != nil {
		return err
	}
	return future.WaitForCompletionRef(ctx, c.Client)
}
