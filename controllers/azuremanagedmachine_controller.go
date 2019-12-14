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
	"github.com/pkg/errors"
	"github.com/sanity-io/litter"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
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
		if !ownerMachine.ObjectMeta.DeletionTimestamp.IsZero() {
			continue
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

	// List all machines to match to pools
	// machineList := &infrav1.AzureManagedMachineList{}
	// if err := r.List(ctx, machineList, client.MatchingLabels(map[string]string{
	// 	clusterv1.MachineClusterLabelName: ownerCluster.Name,
	// })); err != nil {
	// 	log.Info("failed to get machine list from api server")
	// 	return ctrl.Result{}, err
	// }

	// capacity := int32(len(machineList.Items))
	// _ = capacity

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
	managedClusterService, err := NewManagedClustersClient(infraCluster.Spec.SubscriptionID)
	if err != nil {
		return ctrl.Result{}, err
	}

	aksCluster, err := managedClusterService.Get(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name)
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
		_, err = managedClusterService.CreateOrUpdate(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name, desiredAksCluster)
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

	agentPools := *desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles
	// *desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles = *aksCluster.ManagedClusterProperties.AgentPoolProfiles

	log.Info("diffing normalized found cluster with desired")
	ignored := []cmp.Option{
		cmpopts.IgnoreFields(containerservice.ManagedCluster{}, "ManagedClusterProperties.ServicePrincipalProfile"),
		cmpopts.IgnoreFields(containerservice.ManagedCluster{}, "ManagedClusterProperties.AgentPoolProfiles"),
	}
	diff := cmp.Diff(desiredAksCluster, normalized, ignored...)
	if diff != "" {
		fmt.Printf("update required (+new -old):\n%s", diff)
		log.Info("applying update to existing cluster")
		future, err := managedClusterService.CreateOrUpdate(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name, desiredAksCluster)
		if err != nil {
			log.Error(err, "failed update managed cluster")
		}
		if err := future.WaitForCompletionRef(ctx, managedClusterService.Client); err != nil {
			log.Error(err, "error completing cluster operation")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, err
	}

	log.Info("normalized and desired managed cluster matched, no update needed (go-cmp)")

	log.Info("comparing with updated agent pools")
	*desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles = agentPools
	if diff := cmp.Diff(desiredAksCluster, normalized, ignored[:1]...); diff != "" {
		fmt.Printf("update required (+new -old):\n%s", diff)
		log.Info("applying update to existing cluster")
		agentPoolService, err := NewAgentPoolsClient(infraCluster.Spec.SubscriptionID)
		if err != nil {
			return ctrl.Result{}, err
		}
		for _, agentPool := range *desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles {
			newPool := containerservice.AgentPool{
				ManagedClusterAgentPoolProfileProperties: &containerservice.ManagedClusterAgentPoolProfileProperties{
					Count:   agentPool.Count,
					VMSize:  agentPool.VMSize,
					MaxPods: agentPool.MaxPods,
					Type:    containerservice.VirtualMachineScaleSets,
				},
			}
			future, err := agentPoolService.CreateOrUpdate(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name, *agentPool.Name, newPool)
			if err != nil {
				log.Error(err, "err starting operation")
				return ctrl.Result{}, err
			}
			if err := future.WaitForCompletionRef(ctx, agentPoolService.Client); err != nil {
				log.Error(err, "error completing operation")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{Requeue: true}, nil
	}
	log.Info("normalized and desired agent pools matched, no update needed (go-cmp)")

	return ctrl.Result{}, nil
}

func (r *AzureManagedMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.AzureManagedMachine{}).
		Complete(r)
}

/*
	Approximation of reconcile procedure:

	fn reconcile(req) (result, err) {
		machine := getInfraMachine(req)
		cluster := getInfraClusterForMachine(machine)

		aks_have := getManagedCluster()
		aks_want := makeManagedCluster(cluster)

		if !exists(aks_have) {
			// Remove all but first node pool for creation
			aks_want.pools = aks_want.pools[:1]
			create(aks_want)
		}

		pool_diff = differ(aks_have.pools, aks_want.pools)

		if !differ(aks_have, aks_want) {
			return
		}

		if !pool_diff {
			// Only update top level cluster properties the first time around
			aks_want.pools = aks_have.pools
			update(aks_want)
		}

		grow, delete, err := makePools(aks_want.pool, aks_have.pools)
		for pool in grow:
			grow(pool)

		for pool in delete:
			if !default(pool) {
				delete(pool)
			}

		nodes := getNodes()
	}

	fn reconcile(req) (res, err) {
		machine := getMachine(req)
		cluster := getClusterForMachine(machine) // use owner ref to get machine, then label to get cluster
		pool := getPoolForMachine(machine) // machine.Pool is the name already

		operation := getOperation(pool, machine)
		switch operation {
		case provision:
			provision(cluster, pool)
		case grow:
		  	grow(cluster, pool)
		case delete:
		  	delete(cluster, pool)
		case prune:
			prune(cluster, pool, machine)
		default:
			return nil, err
		}
	}
*/

func getMachine(ctx context.Context, log logr.Logger, kubeclient client.Client, req ctrl.Request, machine *infrav1.AzureManagedMachine) (finish bool, err error) {
	if machine == nil {
		machine = &infrav1.AzureManagedMachine{}
	}
	if err := kubeclient.Get(ctx, req.NamespacedName, machine); err != nil {
		log.Info("failed to get infra machine from api server")
		return apierrs.IsNotFound(err), client.IgnoreNotFound(err)
	}
	return false, nil
}

func getOwnerMachine(ctx context.Context, log logr.Logger, kubeclient client.Client, machine *infrav1.AzureManagedMachine) (*clusterv1.Machine, error) {
	ownerMachine, err := util.GetOwnerMachine(ctx, kubeclient, machine.ObjectMeta)
	if err != nil {
		return nil, err
	}
	if ownerMachine == nil {
		return nil, errors.New("Machine Controller has not yet set OwnerRef")
	}
	return ownerMachine, nil
}

func getClusterForMachine(ctx context.Context, log logr.Logger, kubeclient client.Client, machine *infrav1.AzureManagedMachine) (*clusterv1.Cluster, error) {
	// Fetch the owner Machine.
	ownerMachine, err := getOwnerMachine(ctx, log, kubeclient, machine)
	if err != nil {
		return nil, err
	}

	// Fetch the owner Cluster.
	ownerCluster, err := util.GetClusterFromMetadata(ctx, kubeclient, ownerMachine.ObjectMeta)
	if err != nil {
		return nil, errors.Wrap(err, "Machine is missing cluster label or cluster does not exist")
	}

	return ownerCluster, nil
}

func getInfraCluster(ctx context.Context, log logr.Logger, kubeclient client.Client, cluster *clusterv1.Cluster, machine *infrav1.AzureManagedMachine) (*infrav1.AzureManagedCluster, error) {
	infraCluster := &infrav1.AzureManagedCluster{}
	infraClusterName := client.ObjectKey{
		Namespace: machine.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}

	if err := kubeclient.Get(ctx, infraClusterName, infraCluster); err != nil {
		log.Info("infra cluster is not available yet")
		return nil, err
	}

	return infraCluster, nil
}

func getAzureCluster(ctx context.Context, log logr.Logger, kubeclient client.Client, cluster *clusterv1.Cluster, machine *infrav1.AzureManagedMachine) (containerservice.ManagedCluster, error) {
	infraCluster, err := getInfraCluster(ctx, log, kubeclient, cluster, machine)
	if err != nil {
		return containerservice.ManagedCluster{}, err
	}

	managedClusterService, err := NewManagedClustersClient(infraCluster.Spec.SubscriptionID)
	if err != nil {
		return containerservice.ManagedCluster{}, err
	}

	aksCluster, err := managedClusterService.Get(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name)
	return aksCluster, err
}

func getPeerMachines(ctx context.Context, log logr.Logger, kubeclient client.Client, cluster, pool string) error {
	machineList := &infrav1.AzureManagedMachineList{}

	opts := client.MatchingLabels(map[string]string{
		clusterv1.MachineClusterLabelName: cluster,
	})

	if err := kubeclient.List(ctx, machineList, opts); err != nil {
		return errors.Wrap(err, "failed to get machine list from api server")
	}

	n := 0
	for _, machine := range machineList.Items {
		if machine.Spec.Pool == pool {
			machineList.Items[n] = machine
			n++
		}
	}
	machineList.Items = machineList.Items[:n]
	return nil
}
