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
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	operatorsv1alpha1 "github.com/alexeldeib/operators/api/v1alpha1"
	"github.com/alexeldeib/operators/controllers"

	"k8s.io/apimachinery/pkg/runtime"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
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

	initCmd := exec.Command("/helm", "init", "--client-only")

	var outbuf, errbuf bytes.Buffer
	stdoutIn, err := initCmd.StdoutPipe()
	if err != nil {
		setupLog.Error(err, "failed to attach stdout pipe for helm")
	}

	stderrIn, err := initCmd.StderrPipe()
	if err != nil {
		setupLog.Error(err, "failed to attach stderr pipe for helm")
	}

	var errStdout, errStderr error
	stdout := io.MultiWriter(os.Stdout, &outbuf)
	stderr := io.MultiWriter(os.Stderr, &errbuf)

	setupLog.Info("Executing helm initializaion")

	if err = initCmd.Start(); err != nil {
		setupLog.Error(err, "failed to init helm")
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

	err = initCmd.Wait()
	if errStderr != nil {
		setupLog.Error(errStderr, "Unable to log stderr from helm", "errStderr")
	}
	if errStdout != nil {
		setupLog.Error(errStdout, "Unable to log stdout from helm", "errStdout")
	}
	if err != nil {
		// Ok is true if the error is non-nil and indicates the command ran to completion with non-zero exit code.
		if exiterr, ok := err.(*exec.ExitError); ok {
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				setupLog.Error(err, fmt.Sprintf("helm exited with code %d,\n stdout: %s,\n stderr: %s\n", status.ExitStatus(), string(outbuf.Bytes()), string(errbuf.Bytes())))
			}
		} else {
			// Err is non-nil but the error came from waiting/executing rather than from the running command exiting with error.
			setupLog.Error(err, "failed to wait on helm")
		}
	}
	setupLog.Info("successfully init helm")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{Scheme: scheme, MetricsBindAddress: metricsAddr})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	err = (&controllers.HelmReleaseReconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorderFor("HelmRelease"),
		Log:      ctrl.Log.WithName("controllers").WithName("HelmRelease"),
	}).SetupWithManager(mgr)
	if err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "HelmRelease")
		os.Exit(1)
	}
	err = (&controllers.NginxIngressReconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorderFor("NginxIngress"),
		Log:      ctrl.Log.WithName("controllers").WithName("NginxIngress"),
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
