// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package controllers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2019-10-01/containerservice"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/alexeldeib/stringslice"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	"github.com/prometheus/common/log"
	"github.com/sanity-io/litter"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;patch;update

func (r *AzureManagedMachineReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("azuremanagedmachine", req.NamespacedName)
	return reconcileMachine(ctx, log, r.Client, r.Scheme, req)
}

func (r *AzureManagedMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.AzureManagedMachine{}).
		Complete(r)
}

func getInfraMachine(ctx context.Context, log logr.Logger, kubeclient client.Client, req ctrl.Request) (*infrav1.AzureManagedMachine, error) {
	machine := &infrav1.AzureManagedMachine{}
	if err := kubeclient.Get(ctx, req.NamespacedName, machine); err != nil {
		log.Info("failed to get infra machine from api server")
		if apierrs.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return machine, nil
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

func getClusterForMachine(ctx context.Context, log logr.Logger, kubeclient client.Client, ownerMachine *clusterv1.Machine) (*clusterv1.Cluster, error) {
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

func mapNodeToMachine(ctx context.Context, log logr.Logger, kubeclient, remoteclient client.Client, node *corev1.Node, machine *infrav1.AzureManagedMachine) error {
	node.Labels["azure.managed.infrastructure.cluster.x-k8s.io/machine-name"] = machine.Name
	node.Labels["azure.managed.infrastructure.cluster.x-k8s.io/pool-name"] = machine.Spec.Pool
	if err := remoteclient.Update(ctx, node); err != nil {
		return err
	}
	machine.Spec.ProviderID = &node.Spec.ProviderID
	if err := kubeclient.Update(ctx, machine); err != nil {
		return err
	}
	machine.Status.Ready = true
	if err := kubeclient.Status().Update(ctx, machine); err != nil {
		return err
	}
	return nil
}

// NewClusterClient returns a Client for interacting with a remote Cluster using the given scheme for encoding and decoding objects.
func NewClusterClient(c client.Client, cluster *clusterv1.Cluster, scheme *runtime.Scheme) (client.Client, error) {
	restConfig, err := RESTConfig(c, cluster)
	if err != nil {
		return nil, err
	}
	mapper, err := apiutil.NewDynamicRESTMapper(restConfig, apiutil.WithLazyDiscovery)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create a DynamicRESTMapper for Cluster %s/%s", cluster.Namespace, cluster.Name)
	}
	ret, err := client.New(restConfig, client.Options{
		// Scheme: scheme,
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

func reconcileMachine(ctx context.Context, logger logr.Logger, kubeclient client.Client, scheme *runtime.Scheme, req ctrl.Request) (ctrl.Result, error) {
	log := logger.WithValues("azuremanagedmachine", req.NamespacedName)
	_ = log

	infraMachine, err := getInfraMachine(ctx, log, kubeclient, req)
	if err != nil || infraMachine == nil {
		return ctrl.Result{}, err
	}

	if err := addFinalizer(ctx, kubeclient, infraMachine); err != nil {
		return ctrl.Result{}, err
	}

	ownerMachine, err := getOwnerMachine(ctx, log, kubeclient, infraMachine)
	if err != nil {
		return ctrl.Result{}, err
	}

	ownerCluster, err := getClusterForMachine(ctx, log, kubeclient, ownerMachine)
	if err != nil {
		return ctrl.Result{}, err
	}

	infraCluster, err := getInfraCluster(ctx, log, kubeclient, ownerCluster, infraMachine)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !hasPool(infraCluster, infraMachine.Spec.Pool) {
		log.Info("infra machine must reference pool which exists in cluster definition, won't requeue.")
		return ctrl.Result{}, nil
	}

	managedClusterService, err := NewManagedClustersClient(infraCluster.Spec.SubscriptionID)
	if err != nil {
		return ctrl.Result{}, err
	}

	azureCluster, err := managedClusterService.Get(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name)
	if err != nil && !azureCluster.IsHTTPStatus(http.StatusNotFound) {
		return ctrl.Result{}, err
	}

	agentPoolsService, err := NewAgentPoolsClient(infraCluster.Spec.SubscriptionID)
	if err != nil {
		return ctrl.Result{}, err
	}

	done, err := ensureCluster(ctx, log, kubeclient, infraCluster, infraMachine, ownerCluster, azureCluster)
	if err != nil || !done {
		return ctrl.Result{Requeue: !done}, err
	}

	log.Info("setting api endpoints")
	if err := setAPIEndpoints(ctx, kubeclient, azureCluster, infraCluster); err != nil {
		return ctrl.Result{}, err
	}

	if !infraMachine.DeletionTimestamp.IsZero() {
		done, err := removeAzureMachine(ctx, *infraMachine.Spec.ProviderID)
		if err != nil || !done {
			return ctrl.Result{Requeue: !done}, err
		}
		return ctrl.Result{}, removeFinalizer(ctx, kubeclient, infraMachine)
	}

	if infraMachine.Spec.ProviderID != nil {
		return ctrl.Result{}, nil
	}

	azurePool, err := agentPoolsService.Get(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name, infraMachine.Spec.Pool)
	if err != nil && !azurePool.IsHTTPStatus(http.StatusNotFound) {
		return ctrl.Result{}, err
	}

	done, err = ensurePool(ctx, log, infraCluster, infraMachine, azurePool)
	if err != nil || !done {
		return ctrl.Result{Requeue: !done}, err
	}

	remoteClient, err := NewClusterClient(kubeclient, ownerCluster, scheme)
	if err != nil {
		return ctrl.Result{}, err
	}

	nodes, err := getNodes(ctx, kubeclient, ownerCluster, scheme)
	if err != nil {
		return ctrl.Result{}, err
	}

	nodesInPool := getNodesInPool(nodes, infraMachine.Spec.Pool)

	resourceGroup, err := getResourceGroupForNode(nodes[0])
	if err != nil {
		return ctrl.Result{}, err
	}

	vmssList, err := getVMSSForCluster(ctx, infraCluster.Spec.SubscriptionID, resourceGroup)
	if err != nil {
		return ctrl.Result{}, err
	}

	vmss, err := getVMSSForPool(vmssList, infraMachine.Spec.Pool)
	if err != nil {
		return ctrl.Result{}, err
	}

	node := findNodeForMachine(nodesInPool, infraMachine)

	if node == nil {
		// If the number of Kubernetes nodes in this pool is less than the expected VMSS capacity, wait to add a node until stabilization.
		if len(nodesInPool) == int(*vmss.Sku.Capacity) {
			if err := addAzureMachine(ctx, infraCluster.Spec.SubscriptionID, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name, infraMachine.Spec.Pool, azurePool); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, mapNodeToMachine(ctx, log, kubeclient, remoteClient, node, infraMachine)
}

func ensureCluster(ctx context.Context, logger logr.Logger, kubeclient client.Client, infraCluster *infrav1.AzureManagedCluster, infraMachine *infrav1.AzureManagedMachine, ownerCluster *clusterv1.Cluster, azureCluster containerservice.ManagedCluster) (done bool, err error) {
	desiredAksCluster, err := convertCRDToAzure(infraCluster)
	if err != nil {
		return false, err
	}

	managedClusterService, err := NewManagedClustersClient(infraCluster.Spec.SubscriptionID)
	if err != nil {
		return false, err
	}

	if azureCluster.IsHTTPStatus(http.StatusNotFound) {
		log.Info("creating corresponding cluster with single default node pool in azure")
		infraPool := getPool(infraCluster, infraMachine.Spec.Pool)
		machinePools := makeMachinePools([]infrav1.AzureMachinePoolSpec{*infraPool})
		desiredAksCluster.ManagedClusterProperties.AgentPoolProfiles = machinePools
		_, err = managedClusterService.CreateOrUpdate(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name, desiredAksCluster)
		return false, err
	}

	if !isFinished(azureCluster.ProvisioningState) {
		return false, nil
	}

	credentialList, err := managedClusterService.ListClusterAdminCredentials(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name)
	if err != nil {
		return false, err
	}

	if credentialList.Kubeconfigs == nil || len(*credentialList.Kubeconfigs) < 1 {
		return false, errors.New("no kubeconfigs available for the aks cluster")
	}

	// data, err := base64.StdEncoding.EncodeToString()
	// if err != nil {
	// 	return false, err
	// }

	kubeconfig := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name(ownerCluster.Name, secret.Kubeconfig),
			Namespace: ownerCluster.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "ownerCluster",
					Name:       ownerCluster.Name,
					UID:        ownerCluster.UID,
				},
			},
		},
	}

	litter.Dump(kubeconfig.Data[secret.KubeconfigDataName])

	if _, err := controllerutil.CreateOrUpdate(ctx, kubeclient, kubeconfig, func() error {
		kubeconfig.Data = map[string][]byte{
			secret.KubeconfigDataName: []byte(*(*credentialList.Kubeconfigs)[0].Value),
		}
		return nil
	}); err != nil {
		log.Info("failed to create or update kubeconfig")
		return false, err
	}

	log.Info("normalizing found cluster for comparison")
	normalized := normalize(azureCluster)

	ignored := []cmp.Option{
		cmpopts.IgnoreFields(containerservice.ManagedCluster{}, "ManagedClusterProperties.ServicePrincipalProfile"),
		cmpopts.IgnoreFields(containerservice.ManagedCluster{}, "ManagedClusterProperties.AgentPoolProfiles"),
	}

	diff := cmp.Diff(desiredAksCluster, normalized, ignored...)
	if diff == "" {
		log.Info("normalized and desired managed cluster matched, no update needed (go-cmp)")
		return true, nil
	}
	fmt.Printf("update required (+new -old):\n%s", diff)

	log.Info("applying update to existing cluster")
	future, err := managedClusterService.CreateOrUpdate(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name, desiredAksCluster)
	if err != nil {
		log.Error(err, "failed update managed cluster")
		return false, err
	}
	if err := future.WaitForCompletionRef(ctx, managedClusterService.Client); err != nil {
		log.Error(err, "error completing cluster operation")
		return false, err
	}

	return true, nil
}

func ensurePool(ctx context.Context, logger logr.Logger, infraCluster *infrav1.AzureManagedCluster, infraMachine *infrav1.AzureManagedMachine, azurePool containerservice.AgentPool) (done bool, err error) {
	if !isFinished(azurePool.ProvisioningState) {
		return false, nil
	}

	agentPoolsService, err := NewAgentPoolsClient(infraCluster.Spec.SubscriptionID)
	if err != nil {
		return false, err
	}

	infraPool := getPool(infraCluster, infraMachine.Spec.Pool)
	want := makeMachinePool(*infraPool)
	count := int32(1)
	want.Count = &count
	newPool := containerservice.AgentPool{
		ManagedClusterAgentPoolProfileProperties: &containerservice.ManagedClusterAgentPoolProfileProperties{
			Count:   want.Count,
			VMSize:  want.VMSize,
			MaxPods: want.MaxPods,
			Type:    containerservice.VirtualMachineScaleSets,
		},
	}

	if azurePool.IsHTTPStatus(http.StatusNotFound) {
		_, err = agentPoolsService.CreateOrUpdate(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name, infraMachine.Spec.Pool, newPool)
		return false, err
	}

	if diff := cmp.Diff(azurePool.VMSize, newPool.VMSize); diff != "" {
		fmt.Printf("update required (+new -old):\n%s", diff)
		future, err := agentPoolsService.CreateOrUpdate(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name, infraPool.Name, newPool)
		if err != nil {
			log.Error(err, "err starting operation")
			return false, err
		}
		if err := future.WaitForCompletionRef(ctx, agentPoolsService.Client); err != nil {
			log.Error(err, "error completing operation")
			return false, err
		}
	}

	return true, nil
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

func getNodes(ctx context.Context, kubeclient client.Client, ownerCluster *clusterv1.Cluster, scheme *runtime.Scheme) ([]corev1.Node, error) {
	clusterClient, err := NewClusterClient(kubeclient, ownerCluster, scheme)
	if err != nil {
		return nil, err
	}

	nodeList := &corev1.NodeList{}
	if clusterClient.List(ctx, nodeList); err != nil {
		return nil, err
	}

	return nodeList.Items, nil
}

func getVMSSForCluster(ctx context.Context, subscription, resourceGroup string) ([]compute.VirtualMachineScaleSet, error) {
	vmssService, err := NewVirtualMachineScaleSetsClient(subscription)
	if err != nil {
		return nil, err
	}

	iterator, err := vmssService.ListComplete(ctx, resourceGroup)
	list := []compute.VirtualMachineScaleSet{}
	for iterator.NotDone() {
		vmss := iterator.Value()
		list = append(list, vmss)
		if err := iterator.NextWithContext(ctx); err != nil {
			return nil, err
		}
	}
	return list, nil
}

func getVMSSForPool(list []compute.VirtualMachineScaleSet, name string) (compute.VirtualMachineScaleSet, error) {
	for _, vmss := range list {
		tag, ok := vmss.Tags["poolName"]
		if !ok {
			return compute.VirtualMachineScaleSet{}, errors.New("vmss did not have tag for pool name")
		}
		if *tag == name {
			return vmss, nil
		}
	}
	return compute.VirtualMachineScaleSet{}, errors.New("could not find vmss with tag matching pool name")
}

func getResourceGroupForNode(node corev1.Node) (string, error) {
	tokens, err := tokenizeProviderID(node.Spec.ProviderID)
	if err != nil {
		return "", nil
	}
	return tokens[3], nil
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

func getNodesInPool(nodes []corev1.Node, pool string) []corev1.Node {
	result := []corev1.Node{}
	for _, node := range nodes {
		val := node.Labels["agentpool"]
		if val == pool {
			result = append(result, node)
		}
	}
	return result
}

func addAzureMachine(ctx context.Context, subscription, resourceGroup, resourceName, poolName string, azurePool containerservice.AgentPool) error {
	agentPoolsService, err := NewAgentPoolsClient(subscription)
	if err != nil {
		return err
	}

	*azurePool.Count++

	_, err = agentPoolsService.CreateOrUpdate(ctx, resourceGroup, resourceName, poolName, azurePool)
	if err != nil {
		return err
	}
	return nil
}

func removeAzureMachine(ctx context.Context, providerID string) (done bool, err error) {
	tokens, err := tokenizeProviderID(providerID)
	if err != nil {
		return false, err
	}

	subscription, resourceGroup, vmss, instance := tokens[1], tokens[3], tokens[7], tokens[9]

	vmInstanceService, err := NewVirtualMachineScaleSetVMsClient(subscription)
	if err != nil {
		return false, err
	}

	future, err := vmInstanceService.Delete(ctx, resourceGroup, vmss, instance)
	if err != nil {
		if res := future.Response(); res != nil && res.StatusCode == http.StatusNotFound {
			return true, nil
		}
		return false, err
	}

	found, err := vmInstanceService.Get(ctx, resourceGroup, vmss, instance, "")
	if err != nil && found.IsHTTPStatus(http.StatusNotFound) {
		return true, nil
	}

	return false, err
}

func setAPIEndpoints(ctx context.Context, kubeclient client.Client, azureCluster containerservice.ManagedCluster, infraCluster *infrav1.AzureManagedCluster) error {
	merge := infraCluster.DeepCopy()
	merge.Status.APIEndpoints = []infrav1.APIEndpoint{
		{
			Host: *azureCluster.ManagedClusterProperties.Fqdn,
			Port: 443,
		},
	}
	return kubeclient.Status().Patch(ctx, infraCluster, client.MergeFrom(merge))
}

/*
	fn reconcile(req) (res, err) {
		infra machine
		if deleting -> delete
		if cluster doesn't exist, create with this node in pool
		set cluster api endpoints on infra cluster
		if pool doesn't exist, create with 1 node
		if free node in pool, assign to machine
		if nodes == expected, increment vmss size
	}
*/

func reconcileCluster(ctx context.Context, log logr.Logger, infraCluster *infrav1.AzureManagedCluster, infraMachine *infrav1.AzureManagedMachine) (done bool, err error) {
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

	managedClusterService, err := NewManagedClustersClient(infraCluster.Spec.SubscriptionID)
	if err != nil {
		return false, err
	}

	azureCluster, err := managedClusterService.Get(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name)
	if err != nil && !azureCluster.IsHTTPStatus(http.StatusNotFound) {
		return false, err
	}

	if azureCluster.IsHTTPStatus(http.StatusNotFound) {
		azureClusterWant, err := makeAzureCluster(infraCluster)
		if err != nil {
			return false, nil
		}
		azureClusterWant.ManagedClusterProperties.AgentPoolProfiles = &[]containerservice.ManagedClusterAgentPoolProfile{
			containerservice.ManagedClusterAgentPoolProfile{
				VMSize: containerservice.VMSizeTypes(pool.SKU),
				Type:   containerservice.VirtualMachineScaleSets,
				Name:   &pool.Name,
				Count:  to.Int32Ptr(1),
			},
		}
		_, err = managedClusterService.CreateOrUpdate(ctx, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name, azureClusterWant)
		return false, err
	}

	return true, nil
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
