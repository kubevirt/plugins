package plugin

import (
	pluginv1alpha1 "kubevirt.io/api/plugin/v1alpha1"
)

// NodeHookPoint identifies a VM lifecycle event for node-level hooks.
type NodeHookPoint = pluginv1alpha1.NodeHookPoint

const (
	NodeHookPreVMStart          = pluginv1alpha1.NodeHookPreVMStart
	NodeHookPostVMStart         = pluginv1alpha1.NodeHookPostVMStart
	NodeHookOnVMStop            = pluginv1alpha1.NodeHookOnVMStop
	NodeHookPostVMStop          = pluginv1alpha1.NodeHookPostVMStop
	NodeHookPreMigrationSource  = pluginv1alpha1.NodeHookPreMigrationSource
	NodeHookPreMigrationTarget  = pluginv1alpha1.NodeHookPreMigrationTarget
	NodeHookPostMigrationTarget = pluginv1alpha1.NodeHookPostMigrationTarget
)

// AllNodeHookPoints returns every node hook point.
func AllNodeHookPoints() []NodeHookPoint {
	return []NodeHookPoint{
		NodeHookPreVMStart,
		NodeHookPostVMStart,
		NodeHookOnVMStop,
		NodeHookPostVMStop,
		NodeHookPreMigrationSource,
		NodeHookPreMigrationTarget,
		NodeHookPostMigrationTarget,
	}
}
