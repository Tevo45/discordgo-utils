package dgutils

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

/*
 * TODO
 * Register tests
 * tests for errors and negative values
 */

func TestTryConvert(t *testing.T) {
	/* TODO Test for wrong values as well */
	valOf := reflect.ValueOf
	vals := map[string]reflect.Value{
		"8192":      valOf(uint(8192)),         /* uint		*/
		"-3":        valOf(-3),                 /* int		*/
		"yes":       valOf("yes"),              /* string	*/
		"true":      valOf(true),               /* bool		*/
		"false":     valOf(false),              /*			*/
		"2.3":       valOf(2.3),                /* float	*/
		"3.1415926": valOf(float64(3.1415926)), /* double	*/
	}
	for str, val := range vals {
		actual, err := tryConvert(nil, val.Type(), str)
		if err != nil {
			t.Errorf("errored out for value '%v' of expected type '%s'", str, val.Type())
		}
		if val.Interface() != actual.Interface() {
			t.Errorf("expected '%v' but got '%v' for type '%s' (actual type: '%s')", val, actual, val.Type(), actual.Type())
		}
	}
}

func TestInvoke(t *testing.T) {
	/* TODO Find a way to test discordgo types as well */
	stub := MustCommand(
		func(
			s *discordgo.Session, m *discordgo.MessageCreate,
			ui uint, i int, str string, b bool, f float32, d float64, things []string,
		) {
		}, "Test function", nil,
	)
	stub.Invoke(nil, nil, []string{"3", "-2", "hello", "true", "4.5", "3.1415926", "hello", "there"})
}
