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

package storage

import (
	"fmt"
	"os"
	"path/filepath"

	verifier "go.bytebuilders.dev/license-verifier"
	"go.bytebuilders.dev/license-verifier/info"

	"github.com/pkg/errors"
	"k8s.io/klog/v2"
)

func LoadDir(cid, dir string, rb *RecordBook) (*LicenseRegistry, error) {
	caData, err := info.LoadLicenseCA()
	if err != nil {
		return nil, err
	}
	caCert, err := info.ParseCertificate(caData)
	if err != nil {
		return nil, err
	}

	reg := NewLicenseRegistry(rb)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read dir %s", dir)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fmt.Println(entry.Name())
		filename := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to load file %s", filename)
		}

		license, err := verifier.ParseLicense(verifier.ParserOptions{
			ClusterUID: cid,
			CACert:     caCert,
			License:    data,
		})
		if err != nil {
			klog.ErrorS(err, "Skipping", "file", filename)
			continue
		} else {
			reg.Add(license)
		}
	}
	return reg, nil
}
