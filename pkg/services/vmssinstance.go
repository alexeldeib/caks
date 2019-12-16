package services

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	infrav1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
	"github.com/Azure/go-autorest/autorest"
)

// VMSSInstanceService ...
type VMSSInstanceService struct {
	authorizer autorest.Authorizer
}

// NewVMSSInstanceService ...
func NewVMSSInstanceService(authorizer autorest.Authorizer) *VMSSInstanceService {
	return &VMSSInstanceService{
		authorizer,
	}
}

func newVMSSInstanceClient(subscriptionID string, authorizer autorest.Authorizer) (compute.VirtualMachineScaleSetVMsClient, error) {
	client := compute.NewVirtualMachineScaleSetVMsClient(subscriptionID)
	if err := client.AddToUserAgent("cluster-api-provider-aks"); err != nil {
		return compute.VirtualMachineScaleSetVMsClient{}, err
	}
	client.Authorizer = authorizer
	return client, nil
}

// Get ...
func (svc *VMSSInstanceService) Get(ctx context.Context, machine *infrav1.AzureManagedMachine) (compute.VirtualMachineScaleSetVM, error) {
	tokens, err := tokenizeProviderID(*machine.Spec.ProviderID)
	if err != nil {
		return compute.VirtualMachineScaleSetVM{}, err
	}

	subscription, resourceGroup, vmss, instance := tokens[1], tokens[3], tokens[7], tokens[9]

	client, err := newVMSSInstanceClient(subscription, svc.authorizer)
	if err != nil {
		return compute.VirtualMachineScaleSetVM{}, err
	}

	return client.Get(ctx, resourceGroup, vmss, instance, "")
}

// Delete ...
func (svc *VMSSInstanceService) Delete(ctx context.Context, machine *infrav1.AzureManagedMachine) (done bool, err error) {
	tokens, err := tokenizeProviderID(*machine.Spec.ProviderID)
	if err != nil {
		return false, err
	}

	subscription, resourceGroup, vmss, instance := tokens[1], tokens[3], tokens[7], tokens[9]

	client, err := newVMSSInstanceClient(subscription, svc.authorizer)
	if err != nil {
		return false, err
	}

	future, err := client.Delete(ctx, resourceGroup, vmss, instance)
	if err != nil {
		if res := future.Response(); res != nil && res.StatusCode == http.StatusNotFound {
			return true, nil
		}
		return false, err
	}
	return
}

func tokenizeProviderID(providerID string) ([]string, error) {
	pair := strings.Split(providerID, ":")
	if len(pair) < 2 {
		return nil, errors.New("provider id must have format {provider}:///{provider_id_value}")
	}

	tokens := strings.Split(pair[1], "/")
	// 13 == 3 spaces for leading triple slash, plus 10 tokens to describe a VMSS instance ID
	if len(tokens) != 13 {
		return nil, errors.New("expected 10 slash-separated components in azure vmss instance id, plus leading triple slash")
	}

	tokens = tokens[3:]
	return tokens, nil
}
