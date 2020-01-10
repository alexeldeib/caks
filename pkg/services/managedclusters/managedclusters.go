// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.
package managedclusters

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2019-11-01/containerservice"
	"github.com/Azure/go-autorest/autorest"
)

// type Service interface {
// 	Get(ctx context.Context, cluster *infrav1.AzureManagedCluster) (containerservice.ManagedCluster, error)
// 	CreateOrUpdate(ctx context.Context, cluster *infrav1.AzureManagedCluster, azureCluster containerservice.ManagedCluster) (err error)
// 	Delete(ctx context.Context, cluster *infrav1.AzureManagedCluster) (done bool, err error)
// }

// Service ...
type Service struct {
	authorizer autorest.Authorizer
}

// NewService ...
func NewService(authorizer autorest.Authorizer) *Service {
	return &Service{
		authorizer,
	}
}

func newManagedClustersClient(subscriptionID string, authorizer autorest.Authorizer, debug bool) (containerservice.ManagedClustersClient, error) {
	client := containerservice.NewManagedClustersClient(subscriptionID)
	if err := client.AddToUserAgent("cluster-api-provider-aks"); err != nil {
		return containerservice.ManagedClustersClient{}, err
	}
	client.Authorizer = authorizer
	if debug {
		addDebug(&client.Client)
	}
	return client, nil
}

func addDebug(client *autorest.Client) {
	client.RequestInspector = logRequest()
	client.ResponseInspector = logResponse()
}

// logRequest logs full autorest requests for any Azure client.
func logRequest() autorest.PrepareDecorator {
	return func(p autorest.Preparer) autorest.Preparer {
		return autorest.PreparerFunc(func(r *http.Request) (*http.Request, error) {
			r, err := p.Prepare(r)
			if err != nil {
				fmt.Println(err)
			}
			dump, _ := httputil.DumpRequestOut(r, true)
			fmt.Println(string(dump))
			return r, err
		})
	}
}

// logResponse logs full autorest responses for any Azure client.
func logResponse() autorest.RespondDecorator {
	return func(p autorest.Responder) autorest.Responder {
		return autorest.ResponderFunc(func(r *http.Response) error {
			err := p.Respond(r)
			if err != nil {
				fmt.Println(err)
			}
			dump, _ := httputil.DumpResponse(r, true)
			fmt.Println(string(dump))
			return err
		})
	}
}

// Get ...
func (svc *Service) Get(ctx context.Context, subscriptionID, resourceGroup, name string) (*Spec, error) {
	client, err := newManagedClustersClient(subscriptionID, svc.authorizer, true)
	if err != nil {
		return nil, err
	}

	resource, err := client.Get(ctx, resourceGroup, name)
	if resource.IsHTTPStatus(http.StatusNotFound) {
		return NewSpec(), nil
	}
	if err != nil {
		return nil, err
	}

	return &Spec{
		internal: &resource,
	}, err
}

// CreateOrUpdate ...
func (svc *Service) CreateOrUpdate(ctx context.Context, subscriptionID, resourceGroup, name string, spec *Spec) (err error) {
	client, err := newManagedClustersClient(subscriptionID, svc.authorizer, true)
	if err != nil {
		return err
	}
	future, err := client.CreateOrUpdate(ctx, resourceGroup, name, *spec.internal)
	if err != nil {
		return err
	}
	if err := future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return err
	}
	_, err = future.Result(client)
	return err
}

// Delete ...
func (svc *Service) Delete(ctx context.Context, subscriptionID, resourceGroup, name string, spec *Spec) (done bool, err error) {
	client, err := newManagedClustersClient(subscriptionID, svc.authorizer, true)
	if err != nil {
		return false, err
	}
	future, err := client.Delete(ctx, resourceGroup, name)
	if err != nil {
		if res := future.Response(); res != nil && res.StatusCode == http.StatusNotFound {
			return true, nil
		}
		return false, err
	}
	if err := future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return false, err
	}
	return true, nil
}

// GetCredentials ...
func (svc *Service) GetCredentials(ctx context.Context, subscriptionID, resourceGroup, name string, spec *Spec) ([]byte, error) {
	client, err := newManagedClustersClient(subscriptionID, svc.authorizer, true)
	if err != nil {
		return nil, err
	}

	credentialList, err := client.ListClusterAdminCredentials(ctx, resourceGroup, name)
	if err != nil {
		return nil, err
	}

	if credentialList.Kubeconfigs == nil || len(*credentialList.Kubeconfigs) < 1 {
		return nil, errors.New("no kubeconfigs available for the aks cluster")
	}

	return *(*credentialList.Kubeconfigs)[0].Value, nil
}
