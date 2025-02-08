# Control Plane Kubernetes Operator

A Kubernetes operator for [Control Plane](https://controlplane.com). This project allows you to manage many Control
Plane resources using Kubernetes custom resource definitions (CRDs).

## Prerequisites

- A [Kubernetes](https://kubernetes.io) cluster with [Cert Manager](https://cert-manager.io/docs/installation/)
  installed.
- The [Helm](https://helm.sh) CLI
- A [Control Plane](https://controlplane.com) account

## Installation

### Quickstart

If you don't already have a Kubernetes cluster, run the command below to get a local [kind](https://kind.sigs.k8s.io/) cluster up and running.

```shell  
make cluster-quickstart
```

### Manual Installation

If you already have a cluster:

1. Install [Cert Manager](https://cert-manager.io/docs/installation/).
2. (Optional) [Install ArgoCD](https://argo-cd.readthedocs.io/en/stable/getting_started/).
    - If you decide to install Argo later, you'll need to upgrade your Helm installation to enable Argo-specific
      features. To do so, run:

   ```shell 
   helm upgrade cpln-operator cpln/cpln-operator
   ``` 

3. Run the following commands:

   ```shell
   helm repo add cpln https://controlplane-com.github.io/k8s-operator 
   helm install cpln-operator cpln/cpln-operator
   ```

## Granting the Operator Access to Your Control Plane Org

First, provision
a [Control Plane Service Account](https://docs.controlplane.com/reference/serviceaccount#service-account).

1. Create the Service Account as a member of the `superusers` group to start. Later, you can move it to a group with
   narrower permissions if you wish.
2. Create a [key](https://docs.controlplane.com/reference/serviceaccount#service-account-keys), and store it somewhere
   for safekeeping.
3. Run the following command:

   ```shell 
   make install-secret org=your-org-name key=your-service-account-key
   ```

## Usage

Create a custom resource for one of the supported kinds from the list below. The operator will use the secret you
configured to sync the resource with Control Plane.

### Guidelines

- All resources must contain the `org` property, which tells the operator what the target org is and, therefore, which
  secret to use.
- Some kinds also require a `gvc` property, which tells the operator what the target GVC is.
- Consult the [custom resource definitions](chart/templates/crd) for information about the available and required
  fields.
- For GVC-scoped kinds, a namespace per GVC is recommended.
- For org-scoped kinds, a namespace per org is recommended.

## Preventing Resource Deletion

Deleting a Kubernetes resource while the controller is installed and running will remove the corresponding resource from
Control Plane. You can prevent this by adding a special annotation to the Kubernetes resource anytime before deletion,
e.g.:

```yaml
kind: gvc
metadata:
  name: fresh
  namespace: default
  annotations:
    cpln.io/resource-policy: keep
apiVersion: cpln.io/v1
description: fresh
org: kyle-test-org-2
tags: {}
spec:
  loadBalancer:
    dedicated: false
  staticPlacement:
    locationLinks:
      - //location/aws-eu-central-1
```

## Argo CD

The operator integrates closely with [ArgoCD](https://argoproj.github.io/cd/). There is no special configuration needed
for this to work. Once the operator is installed, you can point Argo at a Git repository containing YAML manifests or a
Helm chart, and you're off to the races!

![Argo Example](images/img.png)

### Example Repository

[https://github.com/cuppojoe/argo-example](https://github.com/cuppojoe/argo-example)

## Supported Kinds

The operator supports:

- `agent`
- `auditcontext`
- `domain`
- `group`
- `gvc`
- `identity`
- `ipset`
- `location`
- `policy`
- `volumeset`
- `workload`
- `secret` ([Secrets are a special case](#secrets))

## Secrets

For security reasons, secret data must be stored using a native Kubernetes Secret object, not a custom resource. This
makes it slightly different from the other kinds.

**Note the `app.kubernetes.io/managed-by` label, and the `cpln.io/org` annotation in the example below.**

### Example

```yaml
apiVersion: v1
kind: Secret
type: opaque
metadata:
  labels:
    app.kubernetes.io/managed-by: cpln-operator # Secrets without this label are ignored
  name: secret
  namespace: default
  annotations:
    cpln.io/org: kyle-test-org # Replace this with your org name. This is required.
data:
  encoding: cGxhaW4= # plain
  payload: c2VjcmV0LXZhbHVl # secret-value
```
 
