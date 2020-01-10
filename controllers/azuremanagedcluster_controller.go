// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package controllers

import (
	"context"

	infrav1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
	"github.com/alexeldeib/stringslice"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/Azure/cluster-api-provider-aks/pkg/services/managedclusters"
)

const finalizer = "azuremanagedclusters.infrastructure.cluster.x-k8s.io"

var (
	stateSucceeded = "Succeeded"
	stateFailed    = "Failed"
)

// AzureManagedClusterReconciler reconciles a AzureManagedCluster object
type AzureManagedClusterReconciler struct {
	client.Client
	Log                   logr.Logger
	Scheme                *runtime.Scheme
	ManagedClusterService *managedclusters.Service
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

/*
	Cluster reconcile is effectively a no-op. It only does what is necessary so tha
	the upstream cluster API controllers will continue progressing. All of the
	real work will be done in the machine controller.

	The cluster control sets three values to keep things moving along:
	- infraCluster.Status.Ready = true
	- ownerCluster.Status.Ready = true => this one is a hack because the upstream controller will not do it normally
	- an empty kubeconfig secret with the correct name so capi upstream doesn't try to create one
*/

// Reconcile pretends to reconcile infrastructure for a Kubernetes cluster.
func (r *AzureManagedClusterReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("azuremanagedcluster", req.NamespacedName)

	// Fetch the infra cluster to reconcile
	infraCluster := &infrav1.AzureManagedCluster{}
	if err := r.Get(ctx, req.NamespacedName, infraCluster); err != nil {
		log.Info("error during fetch from api server")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch the owner cluster corresponding to that infra cluster.
	ownerCluster, err := util.GetOwnerCluster(ctx, r.Client, infraCluster.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if ownerCluster == nil {
		log.Info("Cluster Controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("cluster", ownerCluster.Name)

	// Reconcile kubeconfig with dummy empty secret to force cluster controller to progress
	kubeconfig := makeKubeconfig(ownerCluster)
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, kubeconfig, func() error {
		if kubeconfig.Data == nil {
			kubeconfig.Data = map[string][]byte{}
		}
		return nil
	})

	// noop cluster creation -- set status to ready for upstream cluster controller
	infraCluster.Status.Ready = true
	if err := r.Status().Update(ctx, infraCluster); err != nil {
		log.Info("failed to patch infra cluster")
		return ctrl.Result{}, err
	}

	// HACK -- set owner status to ready since capi upstream will look for non-existing control plane machines.
	ownerCluster.Status.ControlPlaneInitialized = true
	if err := r.Status().Update(ctx, ownerCluster); err != nil {
		log.Info("failed to patch owner cluster")
		return ctrl.Result{}, err
	}

	if !infraCluster.DeletionTimestamp.IsZero() {
		if err := r.ManagedClusterService.Delete(ctx, infraCluster.Spec.SubscriptionID, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name); err != nil {
			return ctrl.Result{}, err
		}
		controllerutil.RemoveFinalizer(infraCluster, finalizer)
		return ctrl.Result{}, r.Update(ctx, infraCluster)
	}

	// Add finalizer if missing
	if !stringslice.Has(infraCluster.Finalizers, finalizer) {
		controllerutil.AddFinalizer(infraCluster, finalizer)
		return ctrl.Result{}, r.Update(ctx, infraCluster)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager ...
func (r *AzureManagedClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.AzureManagedCluster{}).
		Complete(r)
}
