# Design Notes

AKS clusters require at least one node pool with one node at time of creation,
and deleting either this node pool or the last machine in it is not possible
without deleting the whole cluster. This imposes some interesting constraints
for implementing a Cluster API provider: AKS cannot separate the creation of the
control plane from the creation of at least 1 node pool and node.

One option is to take a "lazy" approach to reconciliation. During the Cluster
reconcile phase, we no-op everything, set status.ready and status.initialized to
true, and set the kubeconfig secret as an empty value. This works okay but
clashes with the intentions of the Cluster object in upstream Cluster API. 

Another option is to allow the creation of "unmanaged" nodes (i.e., those
created as a result of anything other than a user creating a CAPI machine) so
long as these nodes may be later adopted into the cluster if a user creates a
machine to match. This has the odd effect of potentially having a mix of
managed/unmanaged nodes in your cluster, but if this is undesirable this can be
basically eliminated except for the single node case -- as long as all nodes are
created as CAPI machines, there will only be a mismatch until the first CAPI
machine is created. 

Since this is a required field at cluster creation time, the controller will
default some values if no template is provided for a default node pool. The user
can override these values by providing an AzureMachineTemplateSpec in the
spec.DefaultPoolTemplate of the AzureManagedCluster. 

## Creation Flow

TODO(ace): diagram?

- User creates CAPI cluster + CAKS cluster.
- On reconcile, fetch both. If the cluster doesn't exist, create it with 1 node. 
    - allow for a ref to a machinepool with the template, or default the first nodepool with 1 replica
- if the cluster exists, diff the state (e.g. k8s version) and apply those updates if necessary
- for each node pool, create if necessary, update if spec differs. 

## Machine Pool Flow

- User creates CAPI machine pool + CAPZ machine template.
- CAKS Machine Pool controller fetchs both, create pool from template // update if necessary.
- When provisioning state of agent pool is succeeded, set status.ready == true and status.replicas = spec.replicas