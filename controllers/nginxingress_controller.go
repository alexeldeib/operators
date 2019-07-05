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

package controllers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"text/template"
	"time"

	"github.com/Azure/go-autorest/autorest"
	"github.com/go-logr/logr"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/helm/pkg/helm"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cloudv1alpha1 "github.com/alexeldeib/cloud/api/v1alpha1"
	operatorsv1alpha1 "github.com/alexeldeib/operators/api/v1alpha1"
)

// NginxIngressReconciler reconciles a NginxIngress object
type NginxIngressReconciler struct {
	client.Client
	Log        logr.Logger
	Recorder   record.EventRecorder
	Scheme     *runtime.Scheme
	HelmClient helm.Interface
}

// +kubebuilder:rbac:groups=operators.alexeldeib.xyz,resources=nginxingresses,verbs=get;list;watch;create;delete;update;patch
// +kubebuilder:rbac:groups=operators.alexeldeib.xyz,resources=nginxingresses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=azure.cloud.alexeldeib.xyz,resources=publicips,verbs=get;list;watch;create;update;patch;delete

func (r *NginxIngressReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("nginxingress", req.NamespacedName)

	var nginxIngress operatorsv1alpha1.NginxIngress
	if err := r.Get(ctx, req.NamespacedName, &nginxIngress); err != nil {
		return ctrl.Result{}, ignoreNotFound(err)
	}

	_ = "nginxingress.operators.alexeldeib.xyz"
	// TODO(ace): finalizer deletion logic

	// existing IP and Helm Release
	var existingIP cloudv1alpha1.PublicIP
	var existingIPErr error
	if existingIPErr = r.Get(ctx, req.NamespacedName, &existingIP); existingIPErr != nil {
		if !apierrs.IsNotFound(existingIPErr) {
			return ctrl.Result{RequeueAfter: time.Second * 5}, existingIPErr
		}
	}

	nginxIngress.Status.PublicIPReady = existingIP.Status.ProvisioningState == "Succeeded"

	var existingRelease operatorsv1alpha1.HelmRelease
	var existingReleaseErr error
	if existingReleaseErr = r.Get(ctx, req.NamespacedName, &existingRelease); existingReleaseErr != nil {
		if !apierrs.IsNotFound(existingReleaseErr) {
			return ctrl.Result{}, existingReleaseErr
		}
	}

	// TODO(ace): stop duplicating this logic and implement ProvisioningState == succeeded in a helper
	nginxIngress.Status.HelmReleaseReady = existingRelease.Status.ProvisioningState == "Succeeded"

	// Set status
	log.Info("trying to update status")
	if err := r.Status().Update(ctx, &nginxIngress); err != nil {
		log.Error(err, "unable to update NginxIngress status")
		return ctrl.Result{RequeueAfter: time.Second * 5}, err
	}

	// Reconcile publicIP
	publicIPPrefix := fmt.Sprintf("%s-%s-%s-ingress", nginxIngress.Spec.ResourceGroup, nginxIngress.Namespace, nginxIngress.Name)

	publicIP := cloudv1alpha1.PublicIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nginxIngress.Name,
			Namespace: nginxIngress.Namespace,
		},
		Spec: cloudv1alpha1.PublicIPSpec{
			SubscriptionID:   nginxIngress.Spec.SubscriptionID,
			ResourceGroup:    nginxIngress.Spec.ResourceGroup,
			Location:         nginxIngress.Spec.Location,
			AllocationMethod: "static",
			DomainNameLabel:  publicIPPrefix,
		},
	}

	if err := ctrl.SetControllerReference(&nginxIngress, &publicIP, r.Scheme); err != nil {
		log.Error(err, "unable to set controller owner reference for IP")
		return ctrl.Result{}, err
	}

	if existingIPErr != nil && apierrs.IsNotFound(existingIPErr) {
		if err := r.Create(ctx, &publicIP); err != nil {
			log.Error(err, "unable to create publicIP for nginxRelease", "IP", existingIP)
			return ctrl.Result{}, err
		}
		// Requeue after IP successfully created
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	// End reconcile publicIP

	log.Info("starting to template publicIP")
	// Template in static IP for load balancer
	// TODO(ace): refactor as publicIP.Reconcile, helmRelease.Reconcile, move all peripheral logic to helpers
	var publicIPOverride bytes.Buffer
	publicIPOverrideTmpl := "controller.service.loadBalancerIP={{ .PublicIP }}"
	tmpl := template.Must(template.New("ipoverride").Parse(publicIPOverrideTmpl))

	params := map[string]string{
		"PublicIP": existingIP.Status.IPAddress,
	}
	log.Info("executing template")
	if err := tmpl.Execute(&publicIPOverride, params); err != nil {
		log.Error(err, "Failed templating public IP override")
	}

	overrides := []string{publicIPOverride.String()}

	// Reconcile helm chart for nginx
	helmRelease := operatorsv1alpha1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nginxIngress.Name,
			Namespace: nginxIngress.Namespace,
		},
		Spec: operatorsv1alpha1.HelmReleaseSpec{
			Chart:     "stable/nginx-ingress",
			Overrides: overrides,
		},
	}

	log.Info("Setting controller reference")
	if err := ctrl.SetControllerReference(&nginxIngress, &helmRelease, r.Scheme); err != nil {
		log.Error(err, "unable to set controller owner reference for IP")
		return ctrl.Result{Requeue: true}, err
	}

	log.Info("Creating release if not found..", "releaseErr", existingReleaseErr)
	if existingReleaseErr != nil && apierrs.IsNotFound(existingReleaseErr) {
		log.Info("First create condition passed")
		if err := r.Create(ctx, &helmRelease); err != nil {
			log.Error(err, "unable to create helmRelease for nginxRelease", "IP", helmRelease)
			return ctrl.Result{Requeue: true}, err
		}
		log.Info("release created without errors")
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}

	return ctrl.Result{}, nil
}

func (r *NginxIngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorsv1alpha1.NginxIngress{}).
		Complete(r)
}

//
//
// Helper functions (likely will be refactored and shared)
//
//
func IsNotFound(err error) bool {
	detailedErr, ok := err.(autorest.DetailedError)
	if !ok {
		return false
	}
	if detailedErr.StatusCode == http.StatusNotFound {
		return true
	}
	return false
}

func shouldUpdateIP(actual, desired cloudv1alpha1.PublicIP) bool {
	if actual.Spec.SubscriptionID != desired.Spec.SubscriptionID {
		return true
	}
	if actual.Spec.ResourceGroup != desired.Spec.ResourceGroup {
		return true
	}
	if actual.Spec.Location != desired.Spec.Location {
		return true
	}
	if actual.Spec.DomainNameLabel != desired.Spec.DomainNameLabel {
		return true
	}
	if actual.Spec.AllocationMethod != desired.Spec.AllocationMethod {
		return true
	}
	return false
}

func shouldUpdateHelmRelease(actual, desired operatorsv1alpha1.HelmRelease) bool {
	if actual.Spec.Chart != desired.Spec.Chart {
		return true
	}
	if actual.Spec.Values != desired.Spec.Chart {
		return true
	}
	return false
}
