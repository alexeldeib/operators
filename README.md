# Operator CRDs

The goal of this project is to provide CRDs which allow for orchestration of multiple lower level objects. The lower level objects themselves may be objects reconciled by an external controller, or in some well-defined cases reference implementations will exist here (e,g, HelmRelease).

The initial target set of resources is:
- HelmRelease: allows deploying a fully-customized Helm chart to a given namespace.
- NginxIngress (name needs improvement): deploys an instance of Nginx-Ingress controller with an Azure Load balancer, pre-creating a Standard SKU Static Public IP.

The second target set of resources would be:
- Multicluster Ingress implementation
  - Two "global" options
  	- Front Door
    - Traffic Manager
  - Two "regional" options
  	- Load Balancer
    - Application Gateway
  - 2-tier or 3-tier architecture
  - MultiClusterIngressComponent (terrible name, but to contrast with CRD for CLI which would deploy everything together)
  	- Initial implementation would be CLI to deploy all resources
    - Use configures Ingress Controller on Node Port 80/443 or does SSL termination with Front Door/App Gateway, or they terminate SSL on their backend (on Node Port services)
    - v2 implementation would allow each cluster to build its regional resources and register with existing global/regional resources, or build them as necessary.
- KeyVault synced secret
	- Specify a configMap-like structure of key:value pairs, where value is a fully qualified Azure Resource ID of a Key Vault secret, certificate, or key. 
    - Controller will create a Kubernetes Secret of type generic with the data field populated using the keys from the original structure and the returned values from Key Vault.
    - The initial configMap type structure will be strongly typed to allow for configuration of secret formatting (e.g. I want Key Vault Secret of in /resourcegroup/keyvault/name/secret. Convert it from format X to Y and extract ABC as this value.)
## Prerequisites

This is a [Kubebuilder](https://github.com/kubernetes-sigs/kubebuilder) v2.0.0 project. The prerequisites are the same as for any Kubebuilder project:

- Go
    - https://golang.org/doc/install
- Kustomize
    - https://github.com/kubernetes-sigs/kustomize/blob/master/docs/INSTALL.md
    - go get sigs.k8s.io/kustomize from HEAD
- Docker
    - https://docs.docker.com/install/
- Kubectl
    - https://kubernetes.io/docs/tasks/tools/install-kubectl/
    - Available on most package managers, including those on Windows.
- make (available on Windows via scoop or Chocolatey)
- sed (for templating image name during `make docker-build`)

# Build and Install

Run `make` in the root directory to build the manager binary. 

Run `IMG=yourrepo/yourimg make docker-build docker-push` to build and push an image to the docker repository specified in `IMG`.

Run `make manifests` to generate new CRDs, or `make install` to both generate and apply them via kubectl to the current cluster context.

Run `make deploy` to deploy the manager and the CRDs to the current cluster context, generating any as necessary. The manager will be deployed as a stateful set.
