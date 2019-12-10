// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package controllers

import (
	"context"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2019-10-01/containerservice"
	infrastructurev1alpha2 "github.com/Azure/cluster-api-provider-aks/api/v1alpha2"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
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

	var cluster infrastructurev1alpha2.AzureManagedCluster
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
		log.Info("failed to patch")
		return ctrl.Result{}, err
	}

	ownerCluster.Status.ControlPlaneInitialized = true
	if err := r.Status().Update(ctx, ownerCluster); err != nil {
		log.Info("failed to patch")
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
		For(&infrastructurev1alpha2.AzureManagedCluster{}).
		Complete(r)
}

func NewContainerServicesClient(subscriptionID string) (containerservice.ContainerServicesClient, error) {
	client := containerservice.NewContainerServicesClient(subscriptionID)
	authorizer, err := auth.NewAuthorizerFromFile(azure.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		return containerservice.ContainerServicesClient{}, err
	}
	if err := client.AddToUserAgent("cluster-api-provider-aks"); err != nil {
		return containerservice.ContainerServicesClient{}, err
	}
	client.Authorizer = authorizer
	return client, nil
}

func (r *AzureManagedClusterReconciler) ensure(ctx context.Context, cluster *infrastructurev1alpha2.AzureManagedCluster) error {
	az, err := NewContainerServicesClient(cluster.Spec.SubscriptionID)
	if err != nil {
		return err
	}

	existing, err := az.Get(ctx, cluster.Spec.ResourceGroup, cluster.Name)
	if err != nil && !existing.IsHTTPStatus(http.StatusNotFound) {
		return err
	}

	if existing.IsHTTPStatus(http.StatusNotFound) {
	}

	return nil
}
