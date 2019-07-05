module github.com/alexeldeib/operators

go 1.12

replace k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190404173353-6a84e37a896d

//replace k8s.io/cloud-provider => k8s.io/cloud-provider v0.0.0-20190516232619-2bf8e45c845

replace sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.2.0-beta.1

//replace k8s.io/api => k8s.io/api v0.0.0-20190516230258-a675ac48af67

// replace k8s.io/helm => github.com/alexeldeib/helm v0.0.0-20190530213757-e4ce76a2a063+incompatible

exclude (
	github.com/Sirupsen/logrus v1.1.0
	github.com/Sirupsen/logrus v1.1.1
	github.com/Sirupsen/logrus v1.2.0
	github.com/Sirupsen/logrus v1.3.0
	github.com/Sirupsen/logrus v1.4.0
	github.com/Sirupsen/logrus v1.4.1
	github.com/Sirupsen/logrus v1.4.2
)

require (
	github.com/Azure/go-autorest/autorest v0.2.0
	github.com/Masterminds/goutils v1.1.0 // indirect
	github.com/Masterminds/semver v1.4.2 // indirect
	github.com/Masterminds/sprig v2.18.0+incompatible // indirect
	github.com/alexeldeib/cloud v0.0.0-20190603144559-0cad37135bab
	github.com/cyphar/filepath-securejoin v0.2.2 // indirect
	github.com/go-logr/logr v0.1.0
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/google/uuid v1.1.1 // indirect
	github.com/huandu/xstrings v1.2.0 // indirect
	github.com/onsi/ginkgo v1.8.0
	github.com/onsi/gomega v1.5.0
	github.com/pkg/errors v0.8.1
	golang.org/x/net v0.0.0-20190501004415-9ce7a6920f09
	k8s.io/apimachinery v0.0.0-20190404173353-6a84e37a896d
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/gengo v0.0.0-20190327210449-e17681d19d3a // indirect
	k8s.io/helm v2.14.0+incompatible
	sigs.k8s.io/controller-runtime v0.2.0-beta.1
	sigs.k8s.io/controller-tools v0.2.0-beta.1 // indirect
	vbom.ml/util v0.0.0-20180919145318-efcd4e0f9787 // indirect
)

// replace github.com/alexeldeib/cloud => /home/ace/cloud
