package migration

import (
	"reflect"
	"slices"
	"testing"

	pb "github.com/kubevirt/plugins/api/hooks/v1alpha1"
)

func TestFlagValues(t *testing.T) {
	cases := []struct {
		flag  Flags
		value uint64
	}{
		{Live, 1 << 0},
		{PeerToPeer, 1 << 1},
		{Tunnelled, 1 << 2},
		{PersistDest, 1 << 3},
		{UndefineSource, 1 << 4},
		{Paused, 1 << 5},
		{NonSharedDisk, 1 << 6},
		{NonSharedInc, 1 << 7},
		{ChangeProtection, 1 << 8},
		{Unsafe, 1 << 9},
		{Offline, 1 << 10},
		{Compressed, 1 << 11},
		{AbortOnError, 1 << 12},
		{AutoConverge, 1 << 13},
		{RDMAPinAll, 1 << 14},
		{PostCopy, 1 << 15},
		{TLS, 1 << 16},
		{Parallel, 1 << 17},
		{NonSharedSynchronousWrites, 1 << 18},
		{PostCopyResume, 1 << 19},
		{ZeroCopy, 1 << 20},
	}

	for _, tc := range cases {
		if uint64(tc.flag) != tc.value {
			t.Errorf("expected flag value %d, got %d", tc.value, uint64(tc.flag))
		}
	}
}

func TestFlagsHas(t *testing.T) {
	flags := Live | PeerToPeer | PostCopy

	if !flags.Has(Live) {
		t.Error("expected flags to have Live")
	}

	if !flags.Has(PeerToPeer | PostCopy) {
		t.Error("expected flags to have PeerToPeer and PostCopy")
	}

	if flags.Has(Parallel) {
		t.Error("expected flags not to have Parallel")
	}

	if flags.Has(Live | Parallel) {
		t.Error("expected flags not to have Live and Parallel")
	}

	var zero Flags
	if zero.Has(Live) {
		t.Error("expected zero flags not to have Live")
	}
}

func TestStringRoundTrip(t *testing.T) {
	value, present := StringValue(String("tcp://10.0.0.1:49152"))
	if !present {
		t.Fatal("expected parameter to be present")
	}

	if value != "tcp://10.0.0.1:49152" {
		t.Fatalf("expected value %q, got %q", "tcp://10.0.0.1:49152", value)
	}

	value, present = StringValue(String(""))
	if !present {
		t.Fatal("expected empty string parameter to be present")
	}

	if value != "" {
		t.Fatalf("expected empty value, got %q", value)
	}
}

func TestUint64RoundTrip(t *testing.T) {
	value, present := Uint64Value(Uint64(12582912))
	if !present {
		t.Fatal("expected parameter to be present")
	}

	if value != 12582912 {
		t.Fatalf("expected value 12582912, got %d", value)
	}

	value, present = Uint64Value(Uint64(0))
	if !present {
		t.Fatal("expected zero uint64 parameter to be present")
	}

	if value != 0 {
		t.Fatalf("expected value 0, got %d", value)
	}
}

func TestInt32RoundTrip(t *testing.T) {
	value, present := Int32Value(Int32(2))
	if !present {
		t.Fatal("expected parameter to be present")
	}

	if value != 2 {
		t.Fatalf("expected value 2, got %d", value)
	}

	value, present = Int32Value(Int32(0))
	if !present {
		t.Fatal("expected zero int32 parameter to be present")
	}

	if value != 0 {
		t.Fatalf("expected value 0, got %d", value)
	}
}

func TestStringsRoundTrip(t *testing.T) {
	values, present := StringsValue(Strings("vda", "vdb"))
	if !present {
		t.Fatal("expected parameter to be present")
	}

	if !slices.Equal(values, []string{"vda", "vdb"}) {
		t.Fatalf("expected values [vda vdb], got %v", values)
	}

	values, present = StringsValue(Strings("vda"))
	if !present {
		t.Fatal("expected parameter to be present")
	}

	if !slices.Equal(values, []string{"vda"}) {
		t.Fatalf("expected values [vda], got %v", values)
	}
}

func TestRemovalHelpersReportNotPresent(t *testing.T) {
	if _, present := StringValue(RemoveString()); present {
		t.Error("expected RemoveString to report not present")
	}

	if _, present := Uint64Value(RemoveUint64()); present {
		t.Error("expected RemoveUint64 to report not present")
	}

	if _, present := Int32Value(RemoveInt32()); present {
		t.Error("expected RemoveInt32 to report not present")
	}

	if _, present := StringsValue(RemoveStrings()); present {
		t.Error("expected RemoveStrings to report not present")
	}
}

func TestReadersHandleNil(t *testing.T) {
	if _, present := StringValue(nil); present {
		t.Error("expected nil OptionalString to report not present")
	}

	if _, present := Uint64Value(nil); present {
		t.Error("expected nil OptionalUint64 to report not present")
	}

	if _, present := Int32Value(nil); present {
		t.Error("expected nil OptionalInt32 to report not present")
	}

	if _, present := StringsValue(nil); present {
		t.Error("expected nil StringList to report not present")
	}
}

func TestLibvirtDomainMigrateParametersFieldTypes(t *testing.T) {
	handledTypes := []reflect.Type{
		reflect.TypeOf((*pb.OptionalString)(nil)),
		reflect.TypeOf((*pb.OptionalUint64)(nil)),
		reflect.TypeOf((*pb.OptionalInt32)(nil)),
		reflect.TypeOf((*pb.StringList)(nil)),
	}

	checked := 0
	for _, field := range reflect.VisibleFields(reflect.TypeOf(pb.LibvirtDomainMigrateParameters{})) {
		if field.Tag.Get("protobuf") == "" {
			continue
		}
		checked++

		if !slices.Contains(handledTypes, field.Type) {
			t.Errorf("field %s has type %s, which the migration helpers do not cover", field.Name, field.Type)
		}
	}

	if checked == 0 {
		t.Fatal("no protobuf-tagged fields found in LibvirtDomainMigrateParameters")
	}
}
