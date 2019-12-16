package services

import (
	"fmt"
	"net/http"
	"net/http/httputil"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2019-10-01/containerservice"
	"github.com/Azure/go-autorest/autorest"
)

func addUserAgent(client *autorest.Client) error {
	return client.AddToUserAgent("cluster-api-provider-aks")
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

func newAgentPoolsClient(subscriptionID string, authorizer autorest.Authorizer, debug bool) (containerservice.AgentPoolsClient, error) {
	client := containerservice.NewAgentPoolsClient(subscriptionID)
	if err := addUserAgent(&client.Client); err != nil {
		return containerservice.AgentPoolsClient{}, err
	}
	if debug {
		addDebug(&client.Client)
	}
	client.Authorizer = authorizer
	return client, nil
}

func newManagedClustersClient(subscriptionID string, authorizer autorest.Authorizer, debug bool) (containerservice.ManagedClustersClient, error) {
	client := containerservice.NewManagedClustersClient(subscriptionID)
	if err := addUserAgent(&client.Client); err != nil {
		return containerservice.ManagedClustersClient{}, err
	}
	if debug {
		addDebug(&client.Client)
	}
	client.Authorizer = authorizer
	return client, nil
}

func newVirtualMachineScaleSetsClient(subscriptionID string, authorizer autorest.Authorizer, debug bool) (compute.VirtualMachineScaleSetsClient, error) {
	client := compute.NewVirtualMachineScaleSetsClient(subscriptionID)
	if err := addUserAgent(&client.Client); err != nil {
		return compute.VirtualMachineScaleSetsClient{}, err
	}
	if debug {
		addDebug(&client.Client)
	}
	client.Authorizer = authorizer
	return client, nil
}

func newVirtualMachineScaleSetVMsClient(subscriptionID string, authorizer autorest.Authorizer, debug bool) (compute.VirtualMachineScaleSetVMsClient, error) {
	client := compute.NewVirtualMachineScaleSetVMsClient(subscriptionID)
	if err := addUserAgent(&client.Client); err != nil {
		return compute.VirtualMachineScaleSetVMsClient{}, err
	}
	if debug {
		addDebug(&client.Client)
	}
	client.Authorizer = authorizer
	return client, nil
}
