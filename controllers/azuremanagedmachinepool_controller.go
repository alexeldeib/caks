// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1alpha1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
)

// AzureManagedMachinePoolReconciler reconciles a AzureManagedMachinePool object
type AzureManagedMachinePoolReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedmachinepools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedmachinepools/status,verbs=get;update;patch

func (r *AzureManagedMachinePoolReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("azuremanagedmachinepool", req.NamespacedName)

	// your logic here

	return ctrl.Result{}, nil
}

func (r *AzureManagedMachinePoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.AzureManagedMachinePool{}).
		Complete(r)
}
