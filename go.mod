module github.com/Azure/cluster-api-provider-aks

go 1.13

require (
	github.com/Azure/azure-sdk-for-go v38.0.0+incompatible
	github.com/Azure/go-autorest v13.3.1+incompatible // indirect
	github.com/Azure/go-autorest/autorest v0.9.2
	github.com/Azure/go-autorest/autorest/azure/auth v0.4.1
	github.com/Azure/go-autorest/autorest/to v0.3.0
	github.com/Azure/go-autorest/autorest/validation v0.2.0 // indirect
	github.com/Microsoft/go-winio v0.4.14 // indirect
	github.com/alexeldeib/stringslice v0.0.0-20191023084934-4dfa9e692c9d
	github.com/davecgh/go-spew v1.1.1
	github.com/dnaeon/go-vcr v1.0.1
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-metrics v0.0.1 // indirect
	github.com/go-logr/logr v0.1.0
	github.com/google/addlicense v0.0.0-20191205215950-c6b7f1e7f34a // indirect
	github.com/google/go-cmp v0.3.1
	github.com/lucasjones/reggen v0.0.0-20180717132126-cdb49ff09d77
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/onsi/ginkgo v1.8.0
	github.com/onsi/gomega v1.5.0
	github.com/opencontainers/go-digest v1.0.0-rc1 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/pkg/errors v0.8.1
	github.com/prometheus/common v0.6.0
	github.com/sanity-io/litter v1.2.0
	golang.org/x/crypto v0.0.0-20190611184440-5c40567a22f8
	k8s.io/api v0.0.0-20190918195907-bd6ac527cfd2
	k8s.io/apimachinery v0.0.0-20190913080033-27d36303b655
	k8s.io/client-go v0.0.0-20190918200256-06eb1244587a
	sigs.k8s.io/cluster-api v0.2.7
	sigs.k8s.io/controller-runtime v0.4.0
)

replace (
	k8s.io/api => k8s.io/api v0.0.0-20190918155943-95b840bb6a1f
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20190918161926-8f644eb6e783
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190913080033-27d36303b655
	k8s.io/client-go => k8s.io/client-go v0.0.0-20190918160344-1fbdaa4c8d90
	k8s.io/utils => k8s.io/utils v0.0.0-20190801114015-581e00157fb1
)
