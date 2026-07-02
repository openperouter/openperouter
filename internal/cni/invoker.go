// SPDX-License-Identifier:Apache-2.0

package cni

import (
	"github.com/containernetworking/cni/libcni"
)

type Invoker struct {
	cniConfig  *libcni.CNIConfig
	cacheDir   string
	pluginDirs []string
}

func NewInvoker(pluginDirs []string, cacheDir string) *Invoker {
	return &Invoker{
		cniConfig:  libcni.NewCNIConfigWithCacheDir(pluginDirs, cacheDir, nil),
		cacheDir:   cacheDir,
		pluginDirs: pluginDirs,
	}
}

func (inv *Invoker) PluginDirs() []string {
	return inv.pluginDirs
}
