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
	"context"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/record"
	"k8s.io/helm/pkg/helm"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorsv1alpha1 "github.com/alexeldeib/operators/api/v1alpha1"
)

// NginxIngressReconciler reconciles a NginxIngress object
type NginxIngressReconciler struct {
	client.Client
	Log        logr.Logger
	Recorder   record.EventRecorder
	HelmClient helm.Interface
}

// +kubebuilder:rbac:groups=operators.alexeldeib.xyz,resources=nginxingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operators.alexeldeib.xyz,resources=nginxingresses/status,verbs=get;update;patch

func (r *NginxIngressReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("nginxingress", req.NamespacedName)

	// your logic here

	return ctrl.Result{}, nil
}

func (r *NginxIngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorsv1alpha1.NginxIngress{}).
		Complete(r)
}
