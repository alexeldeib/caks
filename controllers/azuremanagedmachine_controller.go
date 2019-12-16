// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package controllers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2019-10-01/containerservice"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/alexeldeib/stringslice"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	"github.com/prometheus/common/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha2"
	"sigs.k8s.io/cluster-api/util"
	kcfg "sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrav1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
)

// AzureManagedMachineReconciler reconciles a AzureManagedMachine object
type AzureManagedMachineReconciler struct {
	client.Client
	Log                   logr.Logger
	Scheme                *runtime.Scheme
	ManagedClusterService ManagedClusterService
	AgentPoolService      AgentPoolService
	VMSSService           VMSSService
	VMSSInstanceService   VMSSInstanceService
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;patch;update

func (r *AzureManagedMachineReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("azuremanagedmachine", req.NamespacedName)
	_ = ctx
	_ = log
	return r.reconcile(req)
}

func (r *AzureManagedMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.AzureManagedMachine{}).
		Complete(r)
}

/*
	fn reconcile(req) (res, err) {
		get infra machine
		if deleting -> delete
		if cluster doesn't exist, create with 1 node in the requested pool
		set kubeconfig secret
		set cluster api endpoints on infra cluster
		if pool doesn't exist, create with 1 node
		if free node in pool, assign to machine
		if nodes == expected, increment vmss size
	}
*/

func (r *AzureManagedMachineReconciler) reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("azuremanagedmachine", req.NamespacedName)

	log.Info("fetching infra machine")
	infraMachine := &infrav1.AzureManagedMachine{}
	if err := r.Client.Get(ctx, req.NamespacedName, infraMachine); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("fetching owner machine")
	ownerMachine, err := util.GetOwnerMachine(ctx, r.Client, infraMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if ownerMachine == nil {
		log.Info("failed to find upstream owner ref on infra machine")
		return ctrl.Result{Requeue: true}, nil
	}

	log.Info("fetching owner cluster for owner machine")
	ownerCluster, err := util.GetOwnerCluster(ctx, r.Client, ownerMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if ownerCluster == nil {
		log.Info("failed to find cluster owner ref on upstream machine")
		return ctrl.Result{Requeue: true}, nil
	}

	log.Info("fetching infra cluster")
	infraCluster := &infrav1.AzureManagedCluster{}
	infraClusterName := client.ObjectKey{
		Namespace: infraMachine.Namespace,
		Name:      ownerCluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Client.Get(ctx, infraClusterName, infraCluster); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("validating infra machine targets existing pool definition on infra cluster")
	if !hasPool(infraCluster, infraMachine.Spec.Pool) {
		log.Info("infra machine must reference pool which exists in cluster definition, won't requeue.")
		return ctrl.Result{}, nil
	}

	log.Info("reconciling cluster")
	done, err := r.reconcileCluster(ctx, log, infraCluster, infraMachine)
	if err != nil || !done {
		return ctrl.Result{Requeue: !done}, err
	}

	if err := r.Client.Status().Update(ctx, infraCluster); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("reconciling kubeconfig")
	if err := r.reconcileKubeconfig(ctx, ownerCluster, infraCluster); err != nil {
		return ctrl.Result{}, err
	}

	clusterClient, err := NewClusterClient(r.Client, ownerCluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	log.Info("reconciling machine")
	done, err = r.reconcileMachine(ctx, clusterClient, infraCluster, infraMachine)
	if err != nil || !done {
		return ctrl.Result{Requeue: !done}, err
	}

	return ctrl.Result{}, nil
}

func (r *AzureManagedMachineReconciler) reconcileCluster(ctx context.Context, log logr.Logger, infraCluster *infrav1.AzureManagedCluster, infraMachine *infrav1.AzureManagedMachine) (done bool, err error) {
	var pool *infrav1.AzureMachinePoolSpec
	for idx, candidate := range infraCluster.Spec.NodePools {
		if candidate.Name == infraMachine.Spec.Pool {
			pool = &infraCluster.Spec.NodePools[idx]
			break
		}
	}

	if pool == nil {
		log.Info("pool not found in cluster for infra machine, will not requeue")
		return true, nil
	}

	azureCluster, err := r.ManagedClusterService.Get(ctx, infraCluster)
	if err != nil && !azureCluster.IsHTTPStatus(http.StatusNotFound) {
		return false, err
	}

	if !isFinished(azureCluster.ProvisioningState) {
		return false, nil
	}

	azureClusterWant, err := makeAzureCluster(infraCluster)
	if err != nil {
		return false, nil
	}

	if azureCluster.IsHTTPStatus(http.StatusNotFound) {
		azureClusterWant.ManagedClusterProperties.AgentPoolProfiles = &[]containerservice.ManagedClusterAgentPoolProfile{
			containerservice.ManagedClusterAgentPoolProfile{
				VMSize: containerservice.VMSizeTypes(pool.SKU),
				Type:   containerservice.VirtualMachineScaleSets,
				Name:   &pool.Name,
				Count:  to.Int32Ptr(1),
			},
		}
		if err := r.ManagedClusterService.CreateOrUpdate(ctx, infraCluster, azureClusterWant); err != nil {
			return false, err
		}
		return true, nil
	}

	log.Info("normalizing found cluster for comparison")
	normalized := normalize(azureCluster)

	ignored := []cmp.Option{
		cmpopts.IgnoreFields(containerservice.ManagedCluster{}, "ManagedClusterProperties.ServicePrincipalProfile"),
		cmpopts.IgnoreFields(containerservice.ManagedCluster{}, "ManagedClusterProperties.AgentPoolProfiles"),
	}

	diff := cmp.Diff(azureClusterWant, normalized, ignored...)
	if diff == "" {
		log.Info("normalized and desired managed cluster matched, no update needed (go-cmp)")
		return true, nil
	}
	fmt.Printf("update required (+new -old):\n%s", diff)

	log.Info("applying update to existing cluster")
	if err := r.ManagedClusterService.CreateOrUpdate(ctx, infraCluster, azureClusterWant); err != nil {
		log.Error(err, "failed update managed cluster")
		return false, err
	}

	infraCluster.Status.APIEndpoints = []infrav1.APIEndpoint{
		{
			Host: *azureCluster.ManagedClusterProperties.Fqdn,
			Port: 443,
		},
	}

	return true, nil
}

func (r *AzureManagedMachineReconciler) reconcilePool(ctx context.Context, logger logr.Logger, infraCluster *infrav1.AzureManagedCluster, infraMachine *infrav1.AzureManagedMachine, azurePool containerservice.AgentPool) (done bool, err error) {
	if !isFinished(azurePool.ProvisioningState) {
		return false, nil
	}

	infraPool := getPool(infraCluster, infraMachine.Spec.Pool)
	count := int32(1)
	want := containerservice.AgentPool{
		Name: &infraPool.Name,
		ManagedClusterAgentPoolProfileProperties: &containerservice.ManagedClusterAgentPoolProfileProperties{
			Count:  &count,
			VMSize: containerservice.VMSizeTypes(infraPool.SKU),
			Type:   containerservice.VirtualMachineScaleSets,
		},
	}

	if azurePool.IsHTTPStatus(http.StatusNotFound) {
		if err := r.AgentPoolService.CreateOrUpdate(ctx, infraCluster, want); err != nil {
			log.Error(err, "failed to create agent pool")
			return false, err
		}
		return false, err
	}

	if diff := cmp.Diff(azurePool.VMSize, want.VMSize); diff != "" {
		fmt.Printf("update required (+new -old):\n%s", diff)
		if err := r.AgentPoolService.CreateOrUpdate(ctx, infraCluster, want); err != nil {
			log.Error(err, "failed to update agent pool")
			return false, err
		}
	}

	return true, nil
}

func (r *AzureManagedMachineReconciler) reconcileKubeconfig(ctx context.Context, ownerCluster *clusterv1.Cluster, infraCluster *infrav1.AzureManagedCluster) error {
	data, err := r.ManagedClusterService.GetCredentials(ctx, infraCluster)
	if err != nil {
		return err
	}
	kubeconfig := makeKubeconfig(ownerCluster)
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, kubeconfig, func() error {
		kubeconfig.Data = map[string][]byte{
			secret.KubeconfigDataName: data,
		}
		return nil
	})
	return err
}

func (r *AzureManagedMachineReconciler) reconcileMachine(ctx context.Context, clusterClient client.Client, infraCluster *infrav1.AzureManagedCluster, infraMachine *infrav1.AzureManagedMachine) (done bool, err error) {
	if !infraMachine.DeletionTimestamp.IsZero() {
		done, err := r.VMSSInstanceService.Delete(ctx, infraMachine)
		if err != nil || !done {
			return done, err
		}
		return true, removeFinalizer(ctx, r.Client, infraMachine)
	}

	if err := addFinalizer(ctx, r.Client, infraMachine); err != nil {
		return false, err
	}

	nodes, err := getNodes(ctx, clusterClient)
	if err != nil {
		return false, err
	}

	nodesInPool := getNodesInPool(nodes, infraMachine.Spec.Pool)
	node := findNodeForMachine(nodesInPool, infraMachine)

	if node != nil {
		// If the number of Kubernetes nodes in this pool is less than the expected VMSS capacity, wait to add a node until stabilization.
		node.Labels["azure.managed.infrastructure.cluster.x-k8s.io/machine-name"] = infraMachine.Name
		node.Labels["azure.managed.infrastructure.cluster.x-k8s.io/pool-name"] = infraMachine.Spec.Pool
		infraMachine.Spec.ProviderID = &node.Spec.ProviderID
		infraMachine.Status.Ready = true
		if err := clusterClient.Update(ctx, node); err != nil {
			return false, err
		}
		if err := r.Client.Update(ctx, infraMachine); err != nil {
			return false, err
		}
		if err := r.Client.Status().Update(ctx, infraMachine); err != nil {
			return false, err
		}
		return true, nil
	}

	azurePool, err := r.AgentPoolService.Get(ctx, infraCluster, infraMachine)
	if err != nil {
		return false, err
	}

	if len(nodesInPool) == int(*azurePool.Count) {
		*azurePool.Count++
		return false, r.AgentPoolService.CreateOrUpdate(ctx, infraCluster, azurePool)
	}

	return false, nil
}

func addFinalizer(ctx context.Context, kubeclient client.Client, machine *infrav1.AzureManagedMachine) error {
	if !stringslice.Has(machine.Finalizers, clusterv1.MachineFinalizer) {
		machine.Finalizers = append(machine.Finalizers, clusterv1.MachineFinalizer)
		if err := kubeclient.Update(ctx, machine); err != nil {
			return err
		}
	}
	return nil
}

func removeFinalizer(ctx context.Context, kubeclient client.Client, machine *infrav1.AzureManagedMachine) error {
	for idx, val := range machine.Finalizers {
		if val == clusterv1.MachineFinalizer {
			machine.Finalizers = append(machine.Finalizers[:idx], machine.Finalizers[idx+1:]...)
			if err := kubeclient.Update(ctx, machine); err != nil {
				return err
			}
		}
	}
	return nil
}

// TODO(ace): consider switching to a map. Do we *need* ordering guarantees provided by an array?
func getPool(infraCluster *infrav1.AzureManagedCluster, name string) *infrav1.AzureMachinePoolSpec {
	for _, pool := range infraCluster.Spec.NodePools {
		if pool.Name == name {
			return &pool
		}
	}
	return nil
}

// TODO(ace): consider switching to a map. Do we *need* ordering guarantees provided by an array?
func hasPool(infraCluster *infrav1.AzureManagedCluster, pool string) bool {
	for _, got := range infraCluster.Spec.NodePools {
		if got.Name == pool {
			return true
		}
	}
	return false
}

func getNodes(ctx context.Context, kubeclient client.Client) ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	if err := kubeclient.List(ctx, nodeList); err != nil {
		return nil, err
	}

	return nodeList.Items, nil
}

func getNodesInPool(nodes []corev1.Node, pool string) (hasMachine []corev1.Node) {
	result := []corev1.Node{}
	for _, node := range nodes {
		val := node.Labels["agentpool"]
		if val == pool {
			result = append(result, node)
		}
	}
	return result
}

func findNodeForMachine(nodes []corev1.Node, infraMachine *infrav1.AzureManagedMachine) *corev1.Node {
	var chosen *corev1.Node
	for _, node := range nodes {
		machineName, ok := node.Labels["azure.managed.infrastructure.cluster.x-k8s.io/machine-name"]
		if !ok || machineName == infraMachine.Name {
			chosen = node.DeepCopy()
		}
	}
	return chosen
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
		if before.ManagedClusterProperties.AgentPoolProfiles != nil {
			after.ManagedClusterProperties.AgentPoolProfiles = &[]containerservice.ManagedClusterAgentPoolProfile{}
			for _, dirty := range *before.ManagedClusterProperties.AgentPoolProfiles {
				clean := containerservice.ManagedClusterAgentPoolProfile{
					Name:   dirty.Name,
					Count:  dirty.Count,
					VMSize: dirty.VMSize,
					Type:   containerservice.AgentPoolType("VirtualMachineScaleSets"),
				}
				*after.ManagedClusterProperties.AgentPoolProfiles = append(*after.ManagedClusterProperties.AgentPoolProfiles, clean)
			}
		}
	}
	return after
}

// TODO(ace): remove this when we target v1alpha3, it's copied from there.

// NewClusterClient returns a Client for interacting with a remote Cluster using the given scheme for encoding and decoding objects.
func NewClusterClient(c client.Client, cluster *clusterv1.Cluster) (client.Client, error) {
	restConfig, err := RESTConfig(c, cluster)
	if err != nil {
		return nil, err
	}
	mapper, err := apiutil.NewDynamicRESTMapper(restConfig, apiutil.WithLazyDiscovery)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create a DynamicRESTMapper for Cluster %s/%s", cluster.Namespace, cluster.Name)
	}
	ret, err := client.New(restConfig, client.Options{
		Mapper: mapper,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create client for Cluster %s/%s", cluster.Namespace, cluster.Name)
	}

	return ret, nil
}

// RESTConfig returns a configuration instance to be used with a Kubernetes client.
func RESTConfig(c client.Client, cluster *clusterv1.Cluster) (*rest.Config, error) {
	kubeConfig, err := kcfg.FromSecret(c, cluster)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to retrieve kubeconfig secret for Cluster %s/%s", cluster.Namespace, cluster.Name)
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeConfig)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create REST configuration for Cluster %s/%s", cluster.Namespace, cluster.Name)
	}

	return restConfig, nil
}
