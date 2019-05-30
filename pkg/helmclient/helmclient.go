package helmclient

import (
	"fmt"
	"os"

	"k8s.io/helm/pkg/helm"
	helm_env "k8s.io/helm/pkg/helm/environment"
	"k8s.io/helm/pkg/tlsutil"
)

var settings helm_env.EnvSettings

func New() (helm.Interface, error) {
	if settings.TLSCaCertFile == helm_env.DefaultTLSCaCert || settings.TLSCaCertFile == "" {
		settings.TLSCaCertFile = settings.Home.TLSCaCert()
	} else {
		settings.TLSCaCertFile = os.ExpandEnv(settings.TLSCaCertFile)
	}
	if settings.TLSCertFile == helm_env.DefaultTLSCert || settings.TLSCertFile == "" {
		settings.TLSCertFile = settings.Home.TLSCert()
	} else {
		settings.TLSCertFile = os.ExpandEnv(settings.TLSCertFile)
	}
	if settings.TLSKeyFile == helm_env.DefaultTLSKeyFile || settings.TLSKeyFile == "" {
		settings.TLSKeyFile = settings.Home.TLSKey()
	} else {
		settings.TLSKeyFile = os.ExpandEnv(settings.TLSKeyFile)
	}

	options := []helm.Option{helm.Host(settings.TillerHost), helm.ConnectTimeout(settings.TillerConnectionTimeout)}

	if settings.TLSVerify || settings.TLSEnable {
		fmt.Printf("Host=%q, Key=%q, Cert=%q, CA=%q\n", settings.TLSServerName, settings.TLSKeyFile, settings.TLSCertFile, settings.TLSCaCertFile)
		tlsopts := tlsutil.Options{
			ServerName:         settings.TLSServerName,
			KeyFile:            settings.TLSKeyFile,
			CertFile:           settings.TLSCertFile,
			InsecureSkipVerify: true,
		}
		if settings.TLSVerify {
			tlsopts.CaCertFile = settings.TLSCaCertFile
			tlsopts.InsecureSkipVerify = false
		}
		tlscfg, err := tlsutil.ClientConfig(tlsopts)
		if err != nil {
			return &helm.Client{}, err
		}
		options = append(options, helm.WithTLS(tlscfg))
	}
	return helm.NewClient(options...), nil
}

func Settings() helm_env.EnvSettings {
	return settings
}
