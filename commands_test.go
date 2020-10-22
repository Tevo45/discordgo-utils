package dgutils

import (
	"testing"
	"reflect"
)

func TestTryConvert(t *testing.T) {
	// TODO Test for everything
	vals := map[string]reflect.Value{
		"8192": reflect.ValueOf(8192),
		"yes": reflect.ValueOf("yes"),
	}
	for str, val := range vals {
		actual, err := tryConvert(val.Type(), str)
		if err != nil {
			t.Errorf("errored out for value '%v' of expected type '%s'", str, val.Type())
		}
		if val.Interface() != actual.Interface() {
			t.Errorf("expected '%v' but got '%v' for type '%s' (actual type: '%s')", val, actual, val.Type(), actual.Type())
		}
	}
}
