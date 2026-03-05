/*
Copyright 2024.

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

package version

import "runtime/debug"

const devVersion = "(devel)"

// Version returns the module version if available (e.g. from a tagged build),
// falling back to the VCS commit hash (with a "-dirty" suffix if the working
// tree was modified), or panics if build info is not available.
func Version() string {
	build, ok := debug.ReadBuildInfo()
	if !ok {
		panic("failed to read build info")
	}
	if build.Main.Version != devVersion && build.Main.Version != "" {
		return build.Main.Version
	}
	var revision string
	var modified bool
	for _, s := range build.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return devVersion
	}
	if modified {
		return revision + "-dirty"
	}
	return revision
}
