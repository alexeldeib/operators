/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"os"

	operatorsv1alpha1 "github.com/alexeldeib/operators/api/v1alpha1"
	"github.com/alexeldeib/operators/controllers"
	"github.com/alexeldeib/operators/pkg/helmclient"
	"k8s.io/helm/cmd/helm/installer"

	"k8s.io/apimachinery/pkg/runtime"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme              = runtime.NewScheme()
	setupLog            = ctrl.Log.WithName("setup")
	stableRepositoryURL = "https://kubernetes-charts.storage.googleapis.com"
	// This is the IPv4 loopback, not localhost, because we have to force IPv4
	// for Dockerized Helm: https://github.com/kubernetes/helm/issues/1410
	localRepositoryURL = "http://127.0.0.1:8879/charts"
)

func init() {

	operatorsv1alpha1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.Parse()

	ctrl.SetLogger(zap.Logger(true))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{Scheme: scheme, MetricsBindAddress: metricsAddr})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	helmClient, err := helmclient.New()
	if err != nil {
		setupLog.Error(err, "failed to create helm client")
		os.Exit(2)
	}

	if err := installer.Initialize(helmclient.Settings().home, os.Stdout, false, helmclient.Settings(), stableRepositoryURL, localRepositoryURL); err != nil {
		return setupLog.Error(err, "error initializing helm")
		os.Exit(3)
	}

	err = (&controllers.HelmReleaseReconciler{
		Client:     mgr.GetClient(),
		HelmClient: helmClient,
		Recorder:   mgr.GetEventRecorderFor("HelmRelease"),
		Log:        ctrl.Log.WithName("controllers").WithName("HelmRelease"),
	}).SetupWithManager(mgr)
	if err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "HelmRelease")
		os.Exit(1)
	}
	err = (&controllers.NginxIngressReconciler{
		Client:     mgr.GetClient(),
		HelmClient: helmClient,
		Recorder:   mgr.GetEventRecorderFor("NginxIngress"),
		Log:        ctrl.Log.WithName("controllers").WithName("NginxIngress"),
	}).SetupWithManager(mgr)
	if err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NginxIngress")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
