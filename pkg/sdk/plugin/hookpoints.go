package plugin

const (
	PreVMStart          = "PreVMStart"
	PostVMStart         = "PostVMStart"
	PreVMStop           = "PreVMStop"
	PostVMStop          = "PostVMStop"
	PreMigrationSource  = "PreMigrationSource"
	PreMigrationTarget  = "PreMigrationTarget"
	PostMigrationTarget = "PostMigrationTarget"
)

func AllHookPoints() []string {
	return []string{
		PreVMStart,
		PostVMStart,
		PreVMStop,
		PostVMStop,
		PreMigrationSource,
		PreMigrationTarget,
		PostMigrationTarget,
	}
}
