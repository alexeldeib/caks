package controllers

import (
	"fmt"
	goruntime "runtime"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2019-11-01/containerservice"
	infrav1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha2"
	"sigs.k8s.io/cluster-api/util/secret"
)

func makeKubeconfig(cluster *clusterv1.Cluster) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name(cluster.Name, secret.Kubeconfig),
			Namespace: cluster.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Cluster",
					Name:       cluster.Name,
					UID:        cluster.UID,
				},
			},
		},
	}
}

func makeAzureCluster(infraCluster *infrav1.AzureManagedCluster) (containerservice.ManagedCluster, error) {
	settings, err := auth.GetSettingsFromFile()
	if err != nil {
		return containerservice.ManagedCluster{}, err
	}
	return containerservice.ManagedCluster{
		Name:     &infraCluster.Spec.Name,
		Location: &infraCluster.Spec.Location,
		ManagedClusterProperties: &containerservice.ManagedClusterProperties{
			KubernetesVersion: &infraCluster.Spec.Version,
			DNSPrefix:         &infraCluster.Spec.Name,
			LinuxProfile: &containerservice.LinuxProfile{
				AdminUsername: to.StringPtr("azureuser"),
				SSH: &containerservice.SSHConfiguration{
					PublicKeys: &[]containerservice.SSHPublicKey{{KeyData: &infraCluster.Spec.SSHPublicKey}},
				},
			},
			ServicePrincipalProfile: &containerservice.ManagedClusterServicePrincipalProfile{
				ClientID: to.StringPtr(settings.Values[auth.ClientID]),
				Secret:   to.StringPtr(settings.Values[auth.ClientSecret]),
			},
		},
	}, nil
}

func makeAzureMachinePool(name, sku string) containerservice.ManagedClusterAgentPoolProfile {
	return containerservice.ManagedClusterAgentPoolProfile{
		VMSize: containerservice.VMSizeTypes(sku),
		Type:   containerservice.VirtualMachineScaleSets,
		Name:   &name,
	}
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

func isFinished(state *string) bool {
	if state == nil {
		return true
	}
	switch *state {
	case stateFailed:
		return true
	case stateSucceeded:
		return true
	default:
		return false
	}
}

func decorate(log logr.Logger) logr.Logger {
	programCounter, filename, line, _ := goruntime.Caller(1)
	fn := goruntime.FuncForPC(programCounter).Name()
	return log.WithName(fmt.Sprintf("[%s]%s:%d", fn, filename, line))
}

func decorateFailure(log logr.Logger) logr.Logger {
	programCounter, filename, line, _ := goruntime.Caller(1)
	fn := goruntime.FuncForPC(programCounter).Name()
	return log.WithValues("failedAt", fmt.Sprintf("[%s]%s:%d", fn, filename, line))
}
