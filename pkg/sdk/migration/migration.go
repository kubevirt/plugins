// Package migration provides libvirt migration flags and parameter helpers
// for PreMigrationSource hooks.
//
// Parameters use a tri-state encoding. In a request, a set wrapper means the
// parameter is present with that value and a nil field means it is absent.
// In a response, a nil field keeps the parameter's current value, a set
// wrapper overrides it (zero values included), and an empty wrapper or empty
// list removes it. Flags are changed with separate set and clear masks in
// which every other bit keeps its value; a bit present in both masks is an
// error.
package migration

import (
	pb "github.com/kubevirt/plugins/api/hooks/v1alpha1"
)

// Flags is the VIR_MIGRATE_* bitmask libvirt accepts for a migration.
type Flags uint64

// Has reports whether all bits of flag are set in f.
func (f Flags) Has(flag Flags) bool {
	return f&flag == flag
}

const (
	Live                       Flags = 1 << 0  // VIR_MIGRATE_LIVE
	PeerToPeer                 Flags = 1 << 1  // VIR_MIGRATE_PEER2PEER
	Tunnelled                  Flags = 1 << 2  // VIR_MIGRATE_TUNNELLED
	PersistDest                Flags = 1 << 3  // VIR_MIGRATE_PERSIST_DEST
	UndefineSource             Flags = 1 << 4  // VIR_MIGRATE_UNDEFINE_SOURCE
	Paused                     Flags = 1 << 5  // VIR_MIGRATE_PAUSED
	NonSharedDisk              Flags = 1 << 6  // VIR_MIGRATE_NON_SHARED_DISK
	NonSharedInc               Flags = 1 << 7  // VIR_MIGRATE_NON_SHARED_INC
	ChangeProtection           Flags = 1 << 8  // VIR_MIGRATE_CHANGE_PROTECTION
	Unsafe                     Flags = 1 << 9  // VIR_MIGRATE_UNSAFE
	Offline                    Flags = 1 << 10 // VIR_MIGRATE_OFFLINE
	Compressed                 Flags = 1 << 11 // VIR_MIGRATE_COMPRESSED
	AbortOnError               Flags = 1 << 12 // VIR_MIGRATE_ABORT_ON_ERROR
	AutoConverge               Flags = 1 << 13 // VIR_MIGRATE_AUTO_CONVERGE
	RDMAPinAll                 Flags = 1 << 14 // VIR_MIGRATE_RDMA_PIN_ALL
	PostCopy                   Flags = 1 << 15 // VIR_MIGRATE_POSTCOPY
	TLS                        Flags = 1 << 16 // VIR_MIGRATE_TLS
	Parallel                   Flags = 1 << 17 // VIR_MIGRATE_PARALLEL
	NonSharedSynchronousWrites Flags = 1 << 18 // VIR_MIGRATE_NON_SHARED_SYNCHRONOUS_WRITES
	PostCopyResume             Flags = 1 << 19 // VIR_MIGRATE_POSTCOPY_RESUME
	ZeroCopy                   Flags = 1 << 20 // VIR_MIGRATE_ZEROCOPY
)

// String returns an OptionalString that overrides a string parameter with value.
func String(value string) *pb.OptionalString {
	return &pb.OptionalString{Kind: &pb.OptionalString_Value{Value: value}}
}

// Uint64 returns an OptionalUint64 that overrides a uint64 parameter with value.
func Uint64(value uint64) *pb.OptionalUint64 {
	return &pb.OptionalUint64{Kind: &pb.OptionalUint64_Value{Value: value}}
}

// Int32 returns an OptionalInt32 that overrides an int32 parameter with value.
func Int32(value int32) *pb.OptionalInt32 {
	return &pb.OptionalInt32{Kind: &pb.OptionalInt32_Value{Value: value}}
}

// Strings returns a StringList that overrides a repeated string parameter with values.
// Calling it with zero values is equivalent to RemoveStrings(): an empty list
// removes the parameter rather than setting it to an empty value.
//
// For MigrateDisks, removing the list while the NonSharedDisk or NonSharedInc
// flag is set makes libvirt migrate all non-shared disks; to migrate no
// disks, clear the flags (NonSharedDisk | NonSharedInc in ClearFlags) instead.
func Strings(values ...string) *pb.StringList {
	return &pb.StringList{Values: values}
}

// RemoveString returns an OptionalString that removes a string parameter.
func RemoveString() *pb.OptionalString {
	return &pb.OptionalString{}
}

// RemoveUint64 returns an OptionalUint64 that removes a uint64 parameter.
func RemoveUint64() *pb.OptionalUint64 {
	return &pb.OptionalUint64{}
}

// RemoveInt32 returns an OptionalInt32 that removes an int32 parameter.
func RemoveInt32() *pb.OptionalInt32 {
	return &pb.OptionalInt32{}
}

// RemoveStrings returns a StringList that removes a repeated string parameter.
//
// For MigrateDisks, removing the list while the NonSharedDisk or NonSharedInc
// flag is set makes libvirt migrate all non-shared disks; to migrate no
// disks, clear the flags (NonSharedDisk | NonSharedInc in ClearFlags) instead.
func RemoveStrings() *pb.StringList {
	return &pb.StringList{}
}

// StringValue reports the value of a string parameter and whether it is
// present. A parameter is present only when its wrapper is non-nil with the
// value set.
func StringValue(param *pb.OptionalString) (string, bool) {
	if param == nil || param.GetKind() == nil {
		return "", false
	}
	return param.GetValue(), true
}

// Uint64Value reports the value of a uint64 parameter and whether it is
// present. A parameter is present only when its wrapper is non-nil with the
// value set.
func Uint64Value(param *pb.OptionalUint64) (uint64, bool) {
	if param == nil || param.GetKind() == nil {
		return 0, false
	}
	return param.GetValue(), true
}

// Int32Value reports the value of an int32 parameter and whether it is
// present. A parameter is present only when its wrapper is non-nil with the
// value set.
func Int32Value(param *pb.OptionalInt32) (int32, bool) {
	if param == nil || param.GetKind() == nil {
		return 0, false
	}
	return param.GetValue(), true
}

// StringsValue reports the values of a repeated string parameter and whether
// it is present. A parameter is present only when its list is non-nil and
// non-empty; an empty list is the encoding that removes the parameter.
func StringsValue(param *pb.StringList) ([]string, bool) {
	if param == nil || len(param.Values) == 0 {
		return nil, false
	}
	return param.Values, true
}
