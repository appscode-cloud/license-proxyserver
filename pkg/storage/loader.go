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
	"os"
	"path/filepath"
	"time"

	verifier "go.bytebuilders.dev/license-verifier"
	"go.bytebuilders.dev/license-verifier/info"

	"github.com/pkg/errors"
	"k8s.io/klog/v2"
)

func LoadDir(cid, dir string, reg *LicenseRegistry) error {
	caData, err := info.LoadLicenseCA()
	if err != nil {
		return err
	}
	caCert, err := info.ParseCertificate(caData)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return errors.Wrapf(err, "failed to read dir %s", dir)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		dirLink, err := isSymlinkToDir(dir, entry)
		if err != nil {
			return err
		}
		if dirLink {
			continue
		}

		filename := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(filename)
		if err != nil {
			return errors.Wrapf(err, "failed to load file %s", filename)
		}

		license, err := verifier.ParseLicense(verifier.ParserOptions{
			ClusterUID: cid,
			CACert:     caCert,
			License:    data,
		})
		if err != nil {
			klog.ErrorS(err, "Skipping", "file", filename)
			continue
		} else if time.Until(license.NotAfter.Time) >= MinRemainingLife {
			klog.InfoS("adding license",
				"dir", dir,
				"licenseID", license.ID,
				"product", license.ProductLine,
				"plan", license.PlanName,
				"expiry", license.NotAfter.UTC().Format(time.RFC822),
			)
			reg.Add(&license, nil)
		}
	}
	return nil
}

func isSymlinkToDir(dir string, entry os.DirEntry) (bool, error) {
	if entry.Type() != os.ModeSymlink {
		return false, nil
	}
	path, err := filepath.EvalSymlinks(filepath.Join(dir, entry.Name()))
	if err != nil {
		return false, err
	}
	stats, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return stats.IsDir(), nil
}
