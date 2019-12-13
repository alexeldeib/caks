// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package controllers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/sanity-io/litter"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha2"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2019-10-01/containerservice"
	infrav1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
)

// AzureManagedMachineReconciler reconciles a AzureManagedMachine object
type AzureManagedMachineReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;patch;update

func (r *AzureManagedMachineReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("azuremanagedmachine", req.NamespacedName)

	// Fetch the AzureManagedMachine instance.
	infraMachine := &infrav1.AzureManagedMachine{}
	if err := r.Get(ctx, req.NamespacedName, infraMachine); err != nil {
		log.Info("failed to get infra machine from api server")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// List all machines to match to pools
	machineList := &infrav1.AzureManagedMachineList{}
	if err := r.List(ctx, machineList); err != nil {
		log.Info("failed to get machine list from api server")
		return ctrl.Result{}, err
	}

	// Set desired size of all pools
	clusterCapacities := map[string]map[string]int{}
	for _, m := range machineList.Items {
		ownerMachine, err := util.GetOwnerMachine(ctx, r.Client, m.ObjectMeta)
		if err != nil {
			return ctrl.Result{}, err
		}
		if ownerMachine == nil {
			log.Info("Machine Controller has not yet set OwnerRef")
			return ctrl.Result{}, nil
		}
		clusterName := ownerMachine.ObjectMeta.Labels[clusterv1.MachineClusterLabelName]
		if _, ok := clusterCapacities[clusterName]; !ok {
			clusterCapacities[clusterName] = map[string]int{}
		}
		clusterCapacities[clusterName][m.Spec.Pool]++
	}

	// Fetch the owner Machine.
	ownerMachine, err := util.GetOwnerMachine(ctx, r.Client, infraMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if ownerMachine == nil {
		log.Info("Machine Controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	// Fetch the owner Cluster.
	ownerCluster, err := util.GetClusterFromMetadata(ctx, r.Client, ownerMachine.ObjectMeta)
	if err != nil {
		log.Info("Machine is missing cluster label or cluster does not exist")
		return ctrl.Result{}, nil
	}

	// Fetch the AzureManagedCluster
	infraCluster := &infrav1.AzureManagedCluster{}
	infraClusterName := client.ObjectKey{
		Namespace: infraMachine.Namespace,
		Name:      ownerCluster.Spec.InfrastructureRef.Name,
	}

	if err := r.Client.Get(ctx, infraClusterName, infraCluster); err != nil {
		log.Info("infra cluster is not available yet")
		return ctrl.Result{}, nil
	}

	// Fetch AKS cluster
	az, err := NewManagedClustersClient(infraCluster.Spec.SubscriptionID)
	if err != nil {
		return ctrl.Result{}, err
	}

	aksCluster, err := az.Get(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name)
	if err != nil && !aksCluster.IsHTTPStatus(http.StatusNotFound) {
		return ctrl.Result{}, err
	}

	litter.Dump(clusterCapacities)

	// Convert infra CRD to AKS managed cluster
	desiredAksCluster, err := convertCRDToAzure(infraCluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	litter.Dump(infraCluster.Spec)
	litter.Dump(*desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles)

	// We provision node pools lazily: first a user adds a node pool to the AzureManagedCluster object,
	// and once a pool exists there they may target it with newly created machines.
	// Machines targeting non-existent node pools will be ignored.
	// Node pools with zero nodes will be deleted, except if the primary pool would be deleted while other nodes remain.
	n := 0
	for index, pool := range *desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles {
		count := int32(clusterCapacities[ownerCluster.Name][*pool.Name])
		fmt.Printf("count: %d\n", count)
		(*desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles)[index].Count = &count
		if count > 0 {
			(*desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles)[n] = (*desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles)[index]
			n++
		}
	}
	*desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles = (*desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles)[:n]
	litter.Dump(*desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles)

	// If we didn't find any corrsponding AKS cluster, pick a node pool as default and create the cluster.
	// We'll add the rest after initial provisioning. We need to know the default to prevent deletion of
	// all nodes in the default pool.
	if aksCluster.IsHTTPStatus(http.StatusNotFound) {
		log.Info("creating not-found cluster")
		*desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles = (*desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles)[:1]
		_, err = az.CreateOrUpdate(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name, desiredAksCluster)
		return ctrl.Result{Requeue: true}, err
	}

	// Patch status with default node pool to prevent deletion later.
	statusPatch := infraCluster.DeepCopy()
	statusPatch.Status.DefaultNodePool = (*desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles)[0].Name
	if err := r.Status().Patch(ctx, infraCluster, client.MergeFrom(statusPatch)); err != nil {
		return ctrl.Result{}, nil
	}

	if !isFinished(aksCluster.ProvisioningState) {
		log.Info("requeueing to wait for azure provisioning")
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	log.Info("normalizing found cluster for comparison")
	normalized := normalize(aksCluster)

	log.Info("diffing normalized found cluster with desired")
	ignored := cmpopts.IgnoreFields(containerservice.ManagedCluster{}, "ManagedClusterProperties.ServicePrincipalProfile")
	diff := cmp.Diff(desiredAksCluster, normalized, ignored)
	if diff == "" {
		log.Info("normalized and desired matched, no update needed (go-cmp)")
		return ctrl.Result{}, nil
	}

	fmt.Printf("update required (-want +got):\n%s", diff)

	log.Info("applying update to existing cluster")
	_, err = az.CreateOrUpdate(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name, desiredAksCluster)
	return ctrl.Result{Requeue: true}, err
}

func (r *AzureManagedMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.AzureManagedMachine{}).
		Complete(r)
}
