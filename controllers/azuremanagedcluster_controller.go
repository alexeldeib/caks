// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package controllers

import (
	"context"
	"fmt"
	"net/http"
	goruntime "runtime"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2019-10-01/containerservice"
	infrastructurev1alpha1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// AzureManagedClusterReconciler reconciles a AzureManagedCluster object
type AzureManagedClusterReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *AzureManagedClusterReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("azuremanagedcluster", req.NamespacedName)

	var cluster infrastructurev1alpha1.AzureManagedCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		log.Info("error during fetch from api server")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch the Cluster.
	ownerCluster, err := util.GetOwnerCluster(ctx, r.Client, cluster.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if ownerCluster == nil {
		log.Info("Cluster Controller has not yet set OwnerRef")
		return reconcile.Result{}, nil
	}

	log = log.WithValues("cluster", ownerCluster.Name)

	kubeconfig := &corev1.Secret{
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
		Data: map[string][]byte{
			secret.KubeconfigDataName: []byte(""),
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, kubeconfig, func() error { return nil }); err != nil {
		log.Info("failed to create or update kubeconfig")
		return ctrl.Result{}, err
	}

	cluster.Status.Ready = true
	if err := r.Status().Update(ctx, &cluster); err != nil {
		log.Info("failed to patch infra cluster")
		return ctrl.Result{}, err
	}

	ownerCluster.Status.ControlPlaneInitialized = true
	if err := r.Status().Update(ctx, ownerCluster); err != nil {
		log.Info("failed to patch owner cluster")
		return ctrl.Result{}, err
	}

	// if !cluster.DeletionTimestamp.IsZero() {
	// 	log.Info("not implemented")
	// 	os.Exit(1)
	// }

	// Reconcile cluster

	return ctrl.Result{}, nil
}

func (r *AzureManagedClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.AzureManagedCluster{}).
		Complete(r)
}

func NewManagedClustersClient(subscriptionID string) (containerservice.ManagedClustersClient, error) {
	client := containerservice.NewManagedClustersClient(subscriptionID)
	if err := client.AddToUserAgent("cluster-api-provider-aks"); err != nil {
		return containerservice.ManagedClustersClient{}, err
	}
	authorizer, err := auth.NewAuthorizerFromFile(azure.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		return containerservice.ManagedClustersClient{}, err
	}
	client.Authorizer = authorizer
	return client, nil
}

func (r *AzureManagedClusterReconciler) ensure(ctx context.Context, cluster *infrastructurev1alpha1.AzureManagedCluster) error {
	log := decorate(r.Log)

	log.Info("creating cluster client")
	az, err := NewManagedClustersClient(cluster.Spec.SubscriptionID)
	if err != nil {
		return err
	}

	log.Info("fetching existing cluster")
	found, err := az.Get(ctx, cluster.Spec.ResourceGroup, cluster.Name)
	if err != nil && !found.IsHTTPStatus(http.StatusNotFound) {
		return err
	}

	log.Info("constructing desired cluster")
	desired, err := convertCluster(cluster)
	if err != nil {
		return err
	}

	if found.IsHTTPStatus(http.StatusNotFound) {
		log.Info("creating not-found cluster")
		// _, err = az.CreateOrUpdate(ctx, cluster.Spec.ResourceGroup, cluster.Spec.Name, *desired)
		return nil
	}

	log.Info("normalizing found cluster for comparison")
	normalized := normalize(found)
	if equality.Semantic.DeepEqual(normalized, desired) {
		log.Info("normalized and desired matched, no update needed")
		return nil
	}

	log.Info("diffing normalized found cluster with desired")
	if diff := cmp.Diff(desired, normalized); diff != "" {
		fmt.Printf("mismatch (-want +got):\n%s", diff)
	}

	log.Info("applying update to existing cluster")
	// _, err = az.CreateOrUpdate(ctx, cluster.Spec.ResourceGroup, cluster.Spec.Name, *desired)
	return nil
}

func convertCluster(local *infrastructurev1alpha1.AzureManagedCluster) (*containerservice.ManagedCluster, error) {
	settings, err := auth.GetSettingsFromFile()
	if err != nil {
		return nil, err
	}
	return &containerservice.ManagedCluster{
		Name:     &local.Spec.Name,
		Location: &local.Spec.Location,
		ManagedClusterProperties: &containerservice.ManagedClusterProperties{
			KubernetesVersion: &local.Spec.Version,
			DNSPrefix:         &local.Spec.Name,
			LinuxProfile: &containerservice.LinuxProfile{
				AdminUsername: to.StringPtr("azureuser"),
				SSH: &containerservice.SSHConfiguration{
					PublicKeys: &[]containerservice.SSHPublicKey{{KeyData: &local.Spec.SSHPublicKey}},
				},
			},
			AgentPoolProfiles: convertMachinePool(local.Spec.NodePools),
			ServicePrincipalProfile: &containerservice.ManagedClusterServicePrincipalProfile{
				ClientID: to.StringPtr(settings.Values[auth.ClientID]),
				Secret:   to.StringPtr(settings.Values[auth.ClientSecret]),
			},
		},
	}, nil
}

func convertMachinePool(machinePools []infrastructurev1alpha1.AzureMachinePool) *[]containerservice.ManagedClusterAgentPoolProfile {
	var result []containerservice.ManagedClusterAgentPoolProfile
	for _, np := range machinePools {
		result = append(result, containerservice.ManagedClusterAgentPoolProfile{
			Name:   &np.Spec.Name,
			Count:  &np.Spec.Capacity,
			VMSize: containerservice.VMSizeTypes(np.Spec.SKU),
		})
	}
	return &result
}

// normalize constructs a fresh containerservice.ManagedCluster with only the fields set by the controller, not fields defaulted by Azure.
func normalize(before containerservice.ManagedCluster) containerservice.ManagedCluster {
	after := containerservice.ManagedCluster{
		Name:     before.Name,
		Location: before.Location,
	}
	if before.ManagedClusterProperties != nil {
		after.ManagedClusterProperties = &containerservice.ManagedClusterProperties{
			KubernetesVersion:       before.ManagedClusterProperties.KubernetesVersion,
			DNSPrefix:               before.ManagedClusterProperties.DNSPrefix,
			ServicePrincipalProfile: before.ManagedClusterProperties.ServicePrincipalProfile,
			LinuxProfile:            before.ManagedClusterProperties.LinuxProfile,
		}
	}
	return after
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
