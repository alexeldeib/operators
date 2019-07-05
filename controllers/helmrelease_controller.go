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
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorsv1alpha1 "github.com/alexeldeib/operators/api/v1alpha1"
)

// HelmReleaseReconciler reconciles a HelmRelease object
type HelmReleaseReconciler struct {
	client.Client
	Log      logr.Logger
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=operators.alexeldeib.xyz,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operators.alexeldeib.xyz,resources=helmreleases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=list;create
// +kubebuilder:rbac:groups="",resources=pods/portforward,verbs=list;create
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
			historyCmd := exec.Command("/helm", "history", helmRelease.Name)
			var histStderr bytes.Buffer
			historyCmd.Stderr = &histStderr
			err := historyCmd.Run()
			histErr := histStderr.String()
			if err != nil {
				if strings.Trim(histErr, "\n") == strings.Trim(fmt.Sprintf("Error: release: \"%s\" not found", helmRelease.Name), "\n") {
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
				}
				return ctrl.Result{}, errors.Wrap(err, "failed to get helm history ")
			}

			deleteCmd := exec.Command("/helm", "delete", helmRelease.Name, "--purge")

			var outbuf, errbuf bytes.Buffer
			stdoutIn, err := deleteCmd.StdoutPipe()
			if err != nil {
				return ctrl.Result{}, errors.Wrap(err, "failed to attach stdout pipe for helm")
			}

			stderrIn, err := deleteCmd.StderrPipe()
			if err != nil {
				return ctrl.Result{}, errors.Wrap(err, "failed to attach stderr pipe for helm")
			}

			var errStdout, errStderr error
			stdout := io.MultiWriter(os.Stdout, &outbuf)
			stderr := io.MultiWriter(os.Stderr, &errbuf)

			r.Log.Info("Executing helm deletion")

			if err = deleteCmd.Start(); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "failed to start helm")
			}

			// Wait with progress.
			var wg sync.WaitGroup
			wg.Add(1)

			go func() {
				_, errStdout = io.Copy(stdout, stdoutIn)
				wg.Done()
			}()

			_, errStderr = io.Copy(stderr, stderrIn)
			wg.Wait()

			err = deleteCmd.Wait()
			if errStderr != nil {
				r.Log.Error(errStderr, "Unable to log stderr from helm", "errStderr")
			}
			if errStdout != nil {
				r.Log.Error(errStdout, "Unable to log stdout from helm", "errStdout")
			}
			if err != nil {
				// Ok is true if the error is non-nil and indicates the command ran to completion with non-zero exit code.
				if exiterr, ok := err.(*exec.ExitError); ok {
					if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
						return ctrl.Result{}, errors.Wrapf(err, "helm exited with code %d,\n stdout: %s,\n stderr: %s\n", status.ExitStatus(), string(outbuf.Bytes()), string(errbuf.Bytes()))
					}
				} else {
					// Err is non-nil but the error came from waiting/executing rather than from the running command exiting with error.
					return ctrl.Result{}, errors.Wrap(err, "failed to wait on helm")
				}
			}
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
			r.Log.Info("successfully deleted helm release")
			return ctrl.Result{}, nil
		}
	}

	// TODO(ace): set status based on /helm status -o json output + code
	// TODO(ace): diff actual and desired; don't update if not necessary.
	historyCmd := exec.Command("/helm", "history", helmRelease.Name)
	var histStderr bytes.Buffer
	historyCmd.Stderr = &histStderr
	err := historyCmd.Run()
	if err == nil {
		r.Log.Info("Found existing release, will not reconcile (TODO)")
		return ctrl.Result{}, nil
	}

	var createCmd *exec.Cmd
	args := []string{"upgrade", "--install", "--wait", "--force", "--atomic", helmRelease.Name, helmRelease.Spec.Chart, "--namespace", helmRelease.Namespace}

	// This is more or less how config maps work, they model arbitrary data as string and write to a file.
	// TODO(ace): probably create a struct wih 1:1 mapping to nginx values yaml.
	// Their config is quite large, so unless validation becomes a problem will probably stick to this.
	if helmRelease.Spec.Values != "" {
		tmpFile, err := ioutil.TempFile(os.TempDir(), "")
		if err != nil {
			r.Log.Error(err, "Cannot create temporary file for values.yml")
			return ctrl.Result{}, err
		}

		// Remember to clean up the file afterwards
		defer os.Remove(tmpFile.Name())
		if _, err := tmpFile.Write([]byte(helmRelease.Spec.Values)); err != nil {
			r.Log.Error(err, "Failed to write tmpfile with values.yaml")
			return ctrl.Result{}, err
		}
		args = append(args, "-f", tmpFile.Name())
	}

	if helmRelease.Spec.Overrides != nil {
		for _, override := range helmRelease.Spec.Overrides {
			args = append(args, "--set", override)
		}
	}

	createCmd = exec.Command("/helm", args...)
	var outbuf, errbuf bytes.Buffer
	stdoutIn, err := createCmd.StdoutPipe()
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to attach stdout pipe for hem")
	}

	stderrIn, err := createCmd.StderrPipe()
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to attach stderr pipe for hem")
	}

	var errStdout, errStderr error
	stdout := io.MultiWriter(os.Stdout, &outbuf)
	stderr := io.MultiWriter(os.Stderr, &errbuf)

	r.Log.Info("Executing helm")

	if err = createCmd.Start(); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to start helm")
	}

	// Wait with progress.
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		_, errStdout = io.Copy(stdout, stdoutIn)
		wg.Done()
	}()

	_, errStderr = io.Copy(stderr, stderrIn)
	wg.Wait()

	err = createCmd.Wait()
	if errStderr != nil {
		r.Log.Error(errStderr, "Unable to log stderr from helm", "errStderr")
	}
	if errStdout != nil {
		r.Log.Error(errStdout, "Unable to log stdout from helm", "errStdout")
	}
	if err != nil {
		// Ok is true if the error is non-nil and indicates the command ran to completion with non-zero exit code.
		if exiterr, ok := err.(*exec.ExitError); ok {
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				return ctrl.Result{}, errors.Wrapf(err, "helm exited with code %d,\n stdout: %s,\n stderr: %s\n", status.ExitStatus(), string(outbuf.Bytes()), string(errbuf.Bytes()))
			}
		} else {
			// Err is non-nil but the error came from waiting/executing rather than from the running command exiting with error.
			return ctrl.Result{}, errors.Wrap(err, "failed to wait on helm")
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
