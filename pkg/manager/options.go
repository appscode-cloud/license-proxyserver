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

package manager

import (
	"github.com/spf13/pflag"
)

type ManagerOptions struct {
	RegistryFQDN string
	BaseURL      string
	Token        string
	CacheDir     string
}

func NewManagerOptions() *ManagerOptions {
	return &ManagerOptions{}
}

func (s *ManagerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.RegistryFQDN, "registryFQDN", s.RegistryFQDN, "Docker registry FQDN used for license proxyserver image")
	fs.StringVar(&s.BaseURL, "baseURL", s.BaseURL, "License server base url")
	fs.StringVar(&s.Token, "token", s.Token, "License server token")
	fs.StringVar(&s.CacheDir, "cache-dir", s.CacheDir, "Path to license cache directory")
}

func (s *ManagerOptions) Validate() []error {
	return nil
}
