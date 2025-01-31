package common_test

import (
	"os"
	"reflect"
	"testing"

	"github.com/controlplane-com/k8s-operator/pkg/common"
)

func TestGetEnvStr(t *testing.T) {
	// Scenario: Environment variable is not set; should return default value.
	prevVal := os.Getenv("TEST_STR_VAR")
	os.Unsetenv("TEST_STR_VAR")
	defer os.Setenv("TEST_STR_VAR", prevVal)

	got := common.GetEnvStr("TEST_STR_VAR", "defaultVal")
	want := "defaultVal"
	if got != want {
		t.Errorf("GetEnvStr with unset variable = %q, want %q", got, want)
	}

	// Scenario: Environment variable is set; should return the set value.
	os.Setenv("TEST_STR_VAR", "actualVal")
	got = common.GetEnvStr("TEST_STR_VAR", "defaultVal")
	want = "actualVal"
	if got != want {
		t.Errorf("GetEnvStr with set variable = %q, want %q", got, want)
	}
}

func TestGetEnvInt(t *testing.T) {
	// Scenario: Environment variable is not set; should return default value.
	prevVal := os.Getenv("TEST_INT_VAR")
	os.Unsetenv("TEST_INT_VAR")
	defer os.Setenv("TEST_INT_VAR", prevVal)

	got := common.GetEnvInt("TEST_INT_VAR", 42)
	want := 42
	if got != want {
		t.Errorf("GetEnvInt with unset variable = %d, want %d", got, want)
	}

	// Scenario: Environment variable is set to integer; should parse properly.
	os.Setenv("TEST_INT_VAR", "100")
	got = common.GetEnvInt("TEST_INT_VAR", 42)
	want = 100
	if got != want {
		t.Errorf("GetEnvInt with valid variable = %d, want %d", got, want)
	}

	// Scenario: Environment variable is invalid; should return default value.
	os.Setenv("TEST_INT_VAR", "abc")
	got = common.GetEnvInt("TEST_INT_VAR", 42)
	want = 42
	if got != want {
		t.Errorf("GetEnvInt with invalid value = %d, want %d", got, want)
	}
}

func TestGetEnvBool(t *testing.T) {
	// Scenario: Environment variable is not set; should return default value.
	prevVal := os.Getenv("TEST_BOOL_VAR")
	os.Unsetenv("TEST_BOOL_VAR")
	defer os.Setenv("TEST_BOOL_VAR", prevVal)

	got := common.GetEnvBool("TEST_BOOL_VAR", true)
	want := true
	if got != want {
		t.Errorf("GetEnvBool with unset variable = %t, want %t", got, want)
	}

	// Scenario: Environment variable is set to a valid bool.
	os.Setenv("TEST_BOOL_VAR", "false")
	got = common.GetEnvBool("TEST_BOOL_VAR", true)
	want = false
	if got != want {
		t.Errorf("GetEnvBool with valid variable = %t, want %t", got, want)
	}

	// Scenario: Environment variable is set to an invalid bool.
	os.Setenv("TEST_BOOL_VAR", "xyz")
	got = common.GetEnvBool("TEST_BOOL_VAR", true)
	want = true
	if got != want {
		t.Errorf("GetEnvBool with invalid value = %t, want %t", got, want)
	}
}

func TestGetEnvSliceStrings(t *testing.T) {
	prevVal := os.Getenv("TEST_SLICE_STR")
	os.Unsetenv("TEST_SLICE_STR")
	defer os.Setenv("TEST_SLICE_STR", prevVal)

	// Scenario: Environment variable is not set; should return the default slice.
	defVal := []string{"default", "values"}
	got := common.GetEnvSlice[string]("TEST_SLICE_STR", defVal)
	if !reflect.DeepEqual(got, defVal) {
		t.Errorf("GetEnvSlice[string] with unset variable = %v, want %v", got, defVal)
	}

	// Scenario: Environment variable is set; should parse a slice of strings.
	os.Setenv("TEST_SLICE_STR", "alpha, beta, gamma")
	want := []string{"alpha", "beta", "gamma"}
	got = common.GetEnvSlice[string]("TEST_SLICE_STR", defVal)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetEnvSlice[string] with set variable = %v, want %v", got, want)
	}
}

type TestStruct struct {
	Name string
	Age  int
}

func TestGetEnvSliceInts(t *testing.T) {
	prevVal := os.Getenv("TEST_SLICE_INT")
	os.Unsetenv("TEST_SLICE_INT")
	defer os.Setenv("TEST_SLICE_INT", prevVal)

	defVal := []int{1, 2, 3}

	// Scenario: No environment variable exists.
	got := common.GetEnvSlice[int]("TEST_SLICE_INT", defVal)
	if !reflect.DeepEqual(got, defVal) {
		t.Errorf("GetEnvSlice[int] with unset variable = %v, want %v", got, defVal)
	}

	// Scenario: Environment variable is valid.
	os.Setenv("TEST_SLICE_INT", "10,20,30")
	want := []int{10, 20, 30}
	got = common.GetEnvSlice[int]("TEST_SLICE_INT", defVal)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetEnvSlice[int] with set variable = %v, want %v", got, want)
	}

	// Scenario: Environment variable has invalid integer.
	os.Setenv("TEST_SLICE_INT", "10,foo,30")
	got = common.GetEnvSlice[int]("TEST_SLICE_INT", defVal)
	if !reflect.DeepEqual(got, defVal) {
		t.Errorf("GetEnvSlice[int] with invalid item = %v, want default %v", got, defVal)
	}
}

func TestGetEnvSliceBools(t *testing.T) {
	prevVal := os.Getenv("TEST_SLICE_BOOL")
	os.Unsetenv("TEST_SLICE_BOOL")
	defer os.Setenv("TEST_SLICE_BOOL", prevVal)

	defVal := []bool{true, false}

	// Scenario: No environment variable.
	got := common.GetEnvSlice[bool]("TEST_SLICE_BOOL", defVal)
	if !reflect.DeepEqual(got, defVal) {
		t.Errorf("GetEnvSlice[bool] with unset variable = %v, want %v", got, defVal)
	}

	// Scenario: Environment variable is valid.
	os.Setenv("TEST_SLICE_BOOL", "true,false,true")
	want := []bool{true, false, true}
	got = common.GetEnvSlice[bool]("TEST_SLICE_BOOL", defVal)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetEnvSlice[bool] with set variable = %v, want %v", got, want)
	}

	// Scenario: Environment variable has invalid bool.
	os.Setenv("TEST_SLICE_BOOL", "true,not_bool,true")
	got = common.GetEnvSlice[bool]("TEST_SLICE_BOOL", defVal)
	if !reflect.DeepEqual(got, defVal) {
		t.Errorf("GetEnvSlice[bool] with invalid item = %v, want default %v", got, defVal)
	}
}
