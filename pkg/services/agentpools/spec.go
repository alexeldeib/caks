// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.
package agentpools

import (
	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2019-11-01/containerservice"
)

var defaultUser string = "azureuser"
var emptyString string = ""

type SpecOption func(*Spec) *Spec

type Spec struct {
	subscriptionID string
	resourceGroup  string
	cluster        string
	Internal       *containerservice.AgentPool
}

func NewSpec(options ...SpecOption) *Spec {
	result := &Spec{
		Internal: &containerservice.AgentPool{
			ManagedClusterAgentPoolProfileProperties: &containerservice.ManagedClusterAgentPoolProfileProperties{
				Type: containerservice.VirtualMachineScaleSets,
			},
		},
	}
	for _, option := range options {
		result = option(result)
	}
	return result
}

func (s *Spec) Set(options ...SpecOption) {
	for _, option := range options {
		s = option(s)
	}
}

func (s *Spec) Exists() bool {
	return s.Internal.ID != nil
}

func Name(name string) SpecOption {
	return func(o *Spec) *Spec {
		o.Internal.Name = &name
		return o
	}
}

func Cluster(cluster string) SpecOption {
	return func(o *Spec) *Spec {
		o.cluster = cluster
		return o
	}
}

func SubscriptionID(sub string) SpecOption {
	return func(o *Spec) *Spec {
		o.subscriptionID = sub
		return o
	}
}

func ResourceGroup(group string) SpecOption {
	return func(o *Spec) *Spec {
		o.resourceGroup = group
		return o
	}
}

func KubernetesVersion(version string) SpecOption {
	return func(o *Spec) *Spec {
		o.Internal.OrchestratorVersion = &version
		return o
	}
}

func Count(count int32) SpecOption {
	return func(o *Spec) *Spec {
		o.Internal.Count = &count
		return o
	}
}

func SKU(sku string) SpecOption {
	return func(o *Spec) *Spec {
		o.Internal.VMSize = containerservice.VMSizeTypes(sku)
		return o
	}
}

func OSDiskSizeGB(size int32) SpecOption {
	return func(o *Spec) *Spec {
		o.Internal.OsDiskSizeGB = &size
		return o
	}
}
