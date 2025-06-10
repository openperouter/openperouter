// SPDX-License-Identifier:Apache-2.0

package envconfig

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type ImageInfo struct {
	Repo string
	Tag  string
}

type EnvConfig struct {
	Namespace             string
	ControllerImage       ImageInfo
	FRRImage              ImageInfo
	KubeRBacImage         ImageInfo
	FRRMetricsPort        int
	SecureFRRMetricsPort  int
	MetricsPort           int
	SecureMetricsPort     int
	DeployPodMonitors     bool
	DeployServiceMonitors bool
	IsOpenshift           bool
}

func FromEnvironment(isOpenshift bool) (EnvConfig, error) {
	res := EnvConfig{}
	found := false
	res.IsOpenshift = isOpenshift
	res.Namespace, found = os.LookupEnv("OPERATOR_NAMESPACE")
	if !found {
		return EnvConfig{}, errors.New("missing mandatory OPERATOR_NAMESPACE env variable")
	}
	var err error
	res.ControllerImage, err = imageFromEnv("CONTROLLER_IMAGE")
	if err != nil {
		return EnvConfig{}, err
	}

	res.FRRImage, err = imageFromEnv("FRR_IMAGE")
	if err != nil {
		return EnvConfig{}, fmt.Errorf("FRRImage is mandatory for frr mode, %w", err)
	}

	res.KubeRBacImage, err = imageFromEnv("KUBE_RBAC_PROXY_IMAGE")
	if err != nil {
		return EnvConfig{}, err
	}

	res.FRRMetricsPort, err = intValueWithDefault("FRR_METRICS_PORT", 7473)
	if err != nil {
		return EnvConfig{}, err
	}
	res.SecureFRRMetricsPort, err = intValueWithDefault("FRR_HTTPS_METRICS_PORT", 0)
	if err != nil {
		return EnvConfig{}, err
	}
	res.MetricsPort, err = intValueWithDefault("METRICS_PORT", 7472)
	if err != nil {
		return EnvConfig{}, err
	}
	res.SecureMetricsPort, err = intValueWithDefault("HTTPS_METRICS_PORT", 0)
	if err != nil {
		return EnvConfig{}, err
	}

	if os.Getenv("DEPLOY_PODMONITORS") == "true" {
		res.DeployPodMonitors = true
	}
	if os.Getenv("DEPLOY_SERVICEMONITORS") == "true" {
		res.DeployServiceMonitors = true
	}

	err = validate(res)
	if err != nil {
		return EnvConfig{}, err
	}

	return res, nil
}

func validate(config EnvConfig) error {
	if config.DeployPodMonitors && config.DeployServiceMonitors {
		return fmt.Errorf("pod monitors and service monitors are mutually exclusive, only one can be enabled")
	}
	if config.SecureMetricsPort != 0 && !config.DeployServiceMonitors {
		return fmt.Errorf("secureMetricsPort is available only if service monitors are enabled")
	}
	if config.SecureFRRMetricsPort != 0 && !config.DeployServiceMonitors {
		return fmt.Errorf("secureFRRMetricsPort is available only if service monitors are enabled")
	}
	return nil
}

func imageFromEnv(imageEnv string) (ImageInfo, error) {
	res := ImageInfo{}
	value, found := os.LookupEnv(imageEnv)
	if !found {
		return res, fmt.Errorf("%s environment value not set", imageEnv)
	}
	res.Repo, res.Tag = getImageNameTag(value)
	return res, nil
}

func getImageNameTag(envValue string) (string, string) {
	pos := strings.LastIndex(envValue, "/")
	// We assume the last ":" shows up right before the image's tag, and the last "/" just before the image's name.
	// Multiple ":" can be present when the port of the registry is specified and we should include
	// it as part of the repo's url.
	img := strings.Split(envValue[pos+1:], ":")
	repoPath := envValue[:pos+1]

	if len(img) == 1 {
		return repoPath + img[0], ""
	}
	return repoPath + img[0], img[1]
}

func intValueWithDefault(name string, def int) (int, error) {
	val := os.Getenv(name)
	if val != "" {
		res, err := strconv.Atoi(val)
		if err != nil {
			return 0, fmt.Errorf("failed to convert %s from %s to int: %w", val, name, err)
		}
		return res, nil
	}
	return def, nil
}
