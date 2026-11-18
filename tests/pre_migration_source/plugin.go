package main

import (
	"context"
	"errors"
	"log"

	"github.com/kubevirt/plugins/pkg/sdk/migration"
	"github.com/kubevirt/plugins/pkg/sdk/plugin"
)

type preMigrationSourceHook struct{}

func (*preMigrationSourceHook) PreMigrationSource(_ context.Context, req *plugin.PreMigrationSourceRequest) (*plugin.PreMigrationSourceResponse, error) {
	if req.VMI.Labels["sdk-pms-fail"] == "true" {
		return nil, errors.New("intentional PreMigrationSource failure")
	}

	if req.Libvirt != nil {
		_, present := migration.StringValue(req.Libvirt.Params.Uri)
		uriState := "absent"
		if present {
			uriState = "present"
		}
		log.Printf("PreMigrationSource live=%t p2p=%t uri=%s", req.Libvirt.Flags.Has(migration.Live), req.Libvirt.Flags.Has(migration.PeerToPeer), uriState)
	}

	return &plugin.PreMigrationSourceResponse{Libvirt: &plugin.LibvirtMigrationChanges{
		SetFlags: migration.Parallel,
		Params:   &plugin.LibvirtDomainMigrateParameters{ParallelConnections: migration.Int32(2)},
	}}, nil
}

func newPlugin() *plugin.Plugin {
	return plugin.New("test-pre-migration-source").
		WithLauncherHook(plugin.PreMigrationSource(&preMigrationSourceHook{}))
}

func main() {
	newPlugin().Execute()
}
