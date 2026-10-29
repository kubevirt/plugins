package main

import (
	"context"
	"errors"
	"log"
	"os"

	"github.com/kubevirt/plugins/pkg/sdk/plugin"
)

type preBootHook struct{}

func (*preBootHook) PreBoot(_ context.Context, req *plugin.PreBootRequest) (*plugin.PreBootResponse, error) {
	if req.VMI.Labels["sdk-preboot-fail"] == "true" {
		return nil, errors.New("intentional PreBoot failure")
	}

	isoPath, ok := req.CloudInitISO()
	if !ok {
		return nil, nil
	}
	info, err := os.Stat(isoPath)
	if err != nil {
		return nil, err
	}
	log.Printf("PreBoot cloudInitISO=%s size=%d", isoPath, info.Size())
	return nil, nil
}

func newPlugin() *plugin.Plugin {
	return plugin.New("test-pre-boot").
		WithLauncherHook(plugin.PreBoot(&preBootHook{}).WithFailureStrategy(plugin.Fail))
}

func main() {
	newPlugin().Execute()
}
