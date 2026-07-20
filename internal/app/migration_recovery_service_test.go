package app

import (
	"reflect"
	"slices"
	"testing"
)

func TestRecoveryServiceExposesExactCoreSurface(t *testing.T) {
	controller := migrationRecoveryTestController(t, migrationRecoveryTestConfig(t))
	recovery, err := newMigrationRecoveryService(controller)
	if err != nil {
		t.Fatalf("newMigrationRecoveryService() error = %v", err)
	}
	want := []string{"Cancel", "Confirm", "Exit", "Prepare", "Retry", "State"}
	if got := exportedMethodNames(recovery); !slices.Equal(got, want) {
		t.Fatalf("recovery methods = %v, want %v", got, want)
	}
}

func exportedMethodNames(value any) []string {
	typeOf := reflect.TypeOf(value)
	methods := make([]string, 0, typeOf.NumMethod())
	for index := 0; index < typeOf.NumMethod(); index++ {
		methods = append(methods, typeOf.Method(index).Name)
	}
	slices.Sort(methods)
	return methods
}
