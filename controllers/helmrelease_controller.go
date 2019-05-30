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
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	// "github.com/pkg/errors"

	helmutil "github.com/alexeldeib/operators/pkg/helmclient"
	"github.com/go-logr/logr"
	"gopkg.in/yaml.v2"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/tools/record"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/downloader"
	"k8s.io/helm/pkg/getter"
	"k8s.io/helm/pkg/helm"
	rls "k8s.io/helm/pkg/proto/hapi/services"
	"k8s.io/helm/pkg/renderutil"
	"k8s.io/helm/pkg/repo"
	storageerrors "k8s.io/helm/pkg/storage/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorsv1alpha1 "github.com/alexeldeib/operators/api/v1alpha1"
)

// HelmReleaseReconciler reconciles a HelmRelease object
type HelmReleaseReconciler struct {
	client.Client
	Log        logr.Logger
	Recorder   record.EventRecorder
	HelmClient helm.Interface
}

// +kubebuilder:rbac:groups=operators.alexeldeib.xyz,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operators.alexeldeib.xyz,resources=helmreleases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=patch;create

func (r *HelmReleaseReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	_ = r.Log.WithValues("helmrelease", req.NamespacedName)

	var helmRelease operatorsv1alpha1.HelmRelease
	if err := r.Get(ctx, req.NamespacedName, &helmRelease); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, ignoreNotFound(err)
	}

	var _ *rls.GetHistoryResponse
	install := false
	_, err := r.HelmClient.ReleaseHistory(helmRelease.Name, helm.WithMaxHistory(1))
	if err != nil {
		if strings.Contains(err.Error(), storageerrors.ErrReleaseNotFound(helmRelease.Name).Error()) {
			r.Log.Info("Release not found, will attempt to install")
			install = true
		} else {
			r.Log.Error(err, "failed to fetch existing release")
		}
	}

	finalizer := "helm.operators.alexeldeib.xyz"
	if helmRelease.ObjectMeta.DeletionTimestamp.IsZero() {
		if !containsString(helmRelease.ObjectMeta.Finalizers, finalizer) {
			helmRelease.ObjectMeta.Finalizers = append(helmRelease.ObjectMeta.Finalizers, finalizer)
			if err := r.Update(ctx, &helmRelease); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if containsString(helmRelease.ObjectMeta.Finalizers, finalizer) {
			// No-op if we didn't find it initially
			if !install {
				helmRelease.ObjectMeta.Finalizers = removeString(helmRelease.ObjectMeta.Finalizers, finalizer)
				if err := r.Update(ctx, &helmRelease); err != nil {
					r.Recorder.Event(&helmRelease, "Warning", "FailedStatusUpdate", fmt.Sprintf(
						"Could not set status for helm release %s/%s, error: %s\n",
						helmRelease.Namespace,
						helmRelease.Name,
						err.Error(),
					))
					r.Log.Error(err, "failed update status")
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			}
			_, err := r.HelmClient.DeleteRelease(
				helmRelease.Name,
				helm.DeletePurge(true),
				helm.DeleteTimeout(300),
			)
			if err != nil {
				r.Log.Error(err, "failed delete helm release")
				return ctrl.Result{}, err
			}
			r.Log.Info("successfully deleted helm release")
			return ctrl.Result{}, nil
		}
	}

	// TODO(ace): diff actual and desired; don't update if not necessary.

	// cp, err := locateChartPath("", "", "", helmRelease.Spec.Chart, "", false, defaultKeyring(), "", "", "")
	// if err != nil {
	// 	r.Log.Info()
	// 	return ctrl.Result{}, err
	// }

	rawVals, err := yaml.Marshal(helmRelease)
	if err != nil {
		r.Log.Error(err, "failed to marshal values yaml")
		return ctrl.Result{}, err
	}

	//
	//
	// Liberally taken/modified from https://github.com/helm/helm/blob/master/cmd/helm/install.go
	//
	//

	// Name validation
	if msgs := validation.IsDNS1123Subdomain(helmRelease.Name); helmRelease.Name != "" && len(msgs) > 0 {
		return ctrl.Result{}, fmt.Errorf("release name %s is invalid: %s", helmRelease.Name, strings.Join(msgs, ";"))
	}

	chartRequested, err := chartutil.Load("blah")
	if err != nil {
		r.Log.Error(err, "failed to load chart")
		return ctrl.Result{}, err
	}

	if req, err := chartutil.LoadRequirements(chartRequested); err == nil {
		// If checkDependencies returns an error, we have unfulfilled dependencies.
		// As of Helm 2.4.0, this is treated as a stopping condition:
		// https://github.com/kubernetes/helm/issues/2209
		if err := renderutil.CheckDependencies(chartRequested, req); err != nil {
			man := &downloader.Manager{
				ChartPath:  helmRelease.Spec.Chart,
				HelmHome:   helmutil.Settings().Home,
				Keyring:    defaultKeyring(),
				SkipUpdate: false,
				Getters:    getter.All(helmutil.Settings()),
			}
			if err := man.Update(); err != nil {
				return ctrl.Result{}, err
			}

			// Update all dependencies which are present in /charts.
			chartRequested, err = chartutil.Load(helmRelease.Spec.Chart)
			if err != nil {
				return ctrl.Result{}, err
			}
		} else {
			return ctrl.Result{}, err
		}
	} else if err != chartutil.ErrRequirementsNotFound {
		return ctrl.Result{}, fmt.Errorf("cannot load requirements: %v", err)
	}

	if install {
		res, err := r.HelmClient.InstallReleaseFromChart(chartRequested,
			helmRelease.Namespace,
			helm.ValueOverrides(rawVals),
			helm.ReleaseName(helmRelease.Name),
			helm.InstallTimeout(480),
			helm.InstallWait(true),
		)
		if err != nil {
			r.Log.Error(err, "failed to install chart release")
			return ctrl.Result{}, err
		}

		rel := res.GetRelease()
		if rel == nil {
			return ctrl.Result{}, nil
		}
		_, err = r.HelmClient.ReleaseStatus(rel.Name)
		if err != nil {
			r.Log.Error(err, "failed to check release status")
			return ctrl.Result{}, err
		}
	} else {
		_, err := r.HelmClient.UpdateReleaseFromChart(helmRelease.Name,
			chartRequested,
			helm.UpdateValueOverrides(rawVals),
			helm.UpgradeTimeout(480),
			helm.ReuseValues(true),
			helm.UpgradeWait(true),
			helm.UpgradeForce(true),
		)
		if err != nil {
			r.Log.Error(err, "failed to upgrade chart release, rolling back")
			_, rollbackErr := r.HelmClient.RollbackRelease(
				helmRelease.Name,
				helm.RollbackForce(true),
				helm.RollbackTimeout(180),
				helm.RollbackCleanupOnFail(true),
			)
			if rollbackErr != nil {
				r.Log.Error(rollbackErr, "failed to roll back chart release")
				return ctrl.Result{}, fmt.Errorf("Upgrade error: %v\n\n rollback error: %v\n", err, rollbackErr)
			}
			r.Log.Info("rolled back bad deployment")
			return ctrl.Result{}, err
		}

		_, err = r.HelmClient.ReleaseStatus(helmRelease.Name)
		if err != nil {
			r.Log.Error(err, "failed to check release status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *HelmReleaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorsv1alpha1.HelmRelease{}).
		Complete(r)
}

//
//
// Helpers below this line
//
//

func ignoreNotFound(err error) error {
	if apierrs.IsNotFound(err) {
		return nil
	}
	return err
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

//
// From github.com/helm/helm/cmd/helm/install.go
//

func defaultKeyring() string {
	return os.ExpandEnv("$HOME/.gnupg/pubring.gpg")
}

//readFile load a file from the local directory or a remote file with a url.
func readFile(filePath, CertFile, KeyFile, CAFile string) ([]byte, error) {
	u, _ := url.Parse(filePath)
	p := getter.All(helmutil.Settings())

	// FIXME: maybe someone handle other protocols like ftp.
	getterConstructor, err := p.ByScheme(u.Scheme)

	if err != nil {
		return ioutil.ReadFile(filePath)
	}

	getter, err := getterConstructor(filePath, CertFile, KeyFile, CAFile)
	if err != nil {
		return []byte{}, err
	}
	data, err := getter.Get(filePath)
	return data.Bytes(), err
}

// Merges source and destination map, preferring values from the source map
func mergeValues(dest map[string]interface{}, src map[string]interface{}) map[string]interface{} {
	for k, v := range src {
		// If the key doesn't exist already, then just set the key to that value
		if _, exists := dest[k]; !exists {
			dest[k] = v
			continue
		}
		nextMap, ok := v.(map[string]interface{})
		// If it isn't another map, overwrite the value
		if !ok {
			dest[k] = v
			continue
		}
		// Edge case: If the key exists in the destination, but isn't a map
		destMap, isMap := dest[k].(map[string]interface{})
		// If the source map has a map for this key, prefer it
		if !isMap {
			dest[k] = v
			continue
		}
		// If we got to this point, it is a map in both, so merge them
		dest[k] = mergeValues(destMap, nextMap)
	}
	return dest
}

// locateChartPath looks for a chart directory in known places, and returns either the full path or an error.
//
// This does not ensure that the chart is well-formed; only that the requested filename exists.
//
// Order of resolution:
// - current working directory
// - if path is absolute or begins with '.', error out here
// - chart repos in $HELM_HOME
// - URL
//
// If 'verify' is true, this will attempt to also verify the chart.
func locateChartPath(repoURL, username, password, name, version string, verify bool, keyring,
	certFile, keyFile, caFile string) (string, error) {
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if fi, err := os.Stat(name); err == nil {
		abs, err := filepath.Abs(name)
		if err != nil {
			return abs, err
		}
		if verify {
			if fi.IsDir() {
				return "", errors.New("cannot verify a directory")
			}
			if _, err := downloader.VerifyChart(abs, keyring); err != nil {
				return "", err
			}
		}
		return abs, nil
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, ".") {
		return name, fmt.Errorf("path %q not found", name)
	}

	crepo := filepath.Join(helmutil.Settings().Home.Repository(), name)
	if _, err := os.Stat(crepo); err == nil {
		return filepath.Abs(crepo)
	}

	dl := downloader.ChartDownloader{
		HelmHome: helmutil.Settings().Home,
		Out:      os.Stdout,
		Keyring:  keyring,
		Getters:  getter.All(helmutil.Settings()),
		Username: username,
		Password: password,
	}
	if verify {
		dl.Verify = downloader.VerifyAlways
	}
	if repoURL != "" {
		chartURL, err := repo.FindChartInAuthRepoURL(repoURL, username, password, name, version,
			certFile, keyFile, caFile, getter.All(helmutil.Settings()))
		if err != nil {
			return "", err
		}
		name = chartURL
	}

	if _, err := os.Stat(helmutil.Settings().Home.Archive()); os.IsNotExist(err) {
		os.MkdirAll(helmutil.Settings().Home.Archive(), 0744)
	}

	filename, _, err := dl.DownloadTo(name, version, helmutil.Settings().Home.Archive())
	if err == nil {
		lname, err := filepath.Abs(filename)
		if err != nil {
			return filename, err
		}
		return lname, nil
	} else if helmutil.Settings().Debug {
		return filename, err
	}

	return filename, fmt.Errorf("failed to download %q (hint: running `helm repo update` may help)", name)
}
