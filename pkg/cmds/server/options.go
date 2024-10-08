/*
Copyright AppsCode Inc. and Contributors.

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

package server

import (
	"os"

	"go.bytebuilders.dev/license-proxyserver/pkg/apiserver"

	"github.com/pkg/errors"
	"github.com/spf13/pflag"
)

type ExtraOptions struct {
	QPS   float64
	Burst int

	BaseURL               string
	Token                 string
	CAFile                string
	InsecureSkipTLSVerify bool
	LicenseDir            string
	CacheDir              string

	HubKubeconfig    string
	SpokeClusterName string
}

func NewExtraOptions() *ExtraOptions {
	return &ExtraOptions{
		QPS:   1e6,
		Burst: 1e6,
	}
}

func (s *ExtraOptions) AddFlags(fs *pflag.FlagSet) {
	fs.Float64Var(&s.QPS, "qps", s.QPS, "The maximum QPS to the master from this client")
	fs.IntVar(&s.Burst, "burst", s.Burst, "The maximum burst for throttle")
	fs.StringVar(&s.BaseURL, "baseURL", s.BaseURL, "License server base url")
	fs.StringVar(&s.Token, "token", s.Token, "License server token")
	fs.StringVar(&s.CAFile, "ca-file", s.CAFile, "Path to custom CA cert file used to issue appscode.com cert")
	fs.BoolVar(&s.InsecureSkipTLSVerify, "insecure-skip-tls-verify", s.InsecureSkipTLSVerify, "If true, skips verifying appscode.com cert")
	fs.StringVar(&s.LicenseDir, "license-dir", s.LicenseDir, "Path to license directory")
	fs.StringVar(&s.CacheDir, "cache-dir", s.CacheDir, "Path to license cache directory")
	fs.StringVar(&s.HubKubeconfig, "hub-kubeconfig", s.HubKubeconfig, "Path to hub kubeconfig")
	fs.StringVar(&s.SpokeClusterName, "cluster-name", s.SpokeClusterName, "Spoke Cluster name")
}

func (s *ExtraOptions) ApplyTo(cfg *apiserver.ExtraConfig) error {
	cfg.BaseURL = s.BaseURL
	cfg.Token = s.Token
	if s.CAFile != "" {
		caCert, err := os.ReadFile(s.CAFile)
		if err != nil {
			return errors.Wrapf(err, "failed to read CA file %s", s.CAFile)
		}
		cfg.CACert = caCert
	}
	cfg.InsecureSkipTLSVerify = s.InsecureSkipTLSVerify
	cfg.LicenseDir = s.LicenseDir
	cfg.CacheDir = s.CacheDir
	cfg.HubKubeconfig = s.HubKubeconfig
	cfg.SpokeClusterName = s.SpokeClusterName
	cfg.ClientConfig.QPS = float32(s.QPS)
	cfg.ClientConfig.Burst = s.Burst

	return nil
}

func (s *ExtraOptions) Validate() []error {
	return nil
}
