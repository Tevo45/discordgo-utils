package dgutils

import (
	"fmt"
	"reflect"
	"errors"
	"encoding/json"

	"github.com/bwmarrin/discordgo"
)

type Cmd struct {
	Help string
	fn interface{}
	paramTypes []reflect.Type
}

type CmdRegister struct {
	Cmds map[string]*Cmd
	Aliases map[string]string
}

var (
	sessionType = reflect.TypeOf(&discordgo.Session{})
	illegalKinds = map[reflect.Kind]bool{
		reflect.Invalid: true,
		reflect.Uintptr: true,
		reflect.Array: true,
		reflect.Chan: true,
		reflect.Func: true,
		reflect.Interface: true,
		reflect.Map: true,
		reflect.Slice: true,
		reflect.Struct: true,
		reflect.UnsafePointer: true,
	}
)

func Command(fn interface{}, help string) (*Cmd, error) {
	val := reflect.ValueOf(fn)
	if kind := val.Kind(); kind != reflect.Func {
		return nil, fmt.Errorf("Command: expected fn of kind Func, got %s", kind)
	}
	ttype := val.Type()
	if ttype.NumIn() < 1 {
		return nil, errors.New("Command: fn takes no arguments, expected 1 or more")
	}
	/* Can we compare pointer types like that? */
	if first := ttype.In(0); first != sessionType {
		return nil, errors.New("Command: fn's first argument is not a pointer to a discordgo.Session")
	}
	var params []reflect.Type
	for c := 1; c < ttype.NumIn(); c++ {
		param := ttype.In(c)
		if kind := param.Kind(); illegalKinds[kind] {
			return nil, fmt.Errorf("Command: argument of kind %s not supported", kind)
		}
		params = append(params, param)
	}
	return &Cmd{Help: help, fn: fn, paramTypes: params}, nil
}

func (cmd *Cmd) Invoke(s *discordgo.Session, args []string) error {
	if len(args) != len(cmd.paramTypes) {
		return errors.New("Cmd.Invoke: argument-parameter count mismatch")
	}
	var vals []reflect.Value
	vals = append(vals, reflect.ValueOf(s))
	for c := 0; c < len(args); c++ {
		expect := cmd.paramTypes[c]
		val, err := tryConvert(expect, args[c])
		if err != nil {
			return err
		}
		vals = append(vals, val)
	}
	reflect.ValueOf(cmd.fn).Call(vals)
	return nil
}

func (reg *CmdRegister) Canon(name string) string {
	canon := reg.Aliases(name)
	if canon != "" {
		return canon
	}
	return name
}

func (reg *CmdRegister) Get(name string) *Cmd {
	return reg.Cmds[reg.Canon(name)]
}

func (reg *CmdRegister) Add(name string, cmd *Cmd) error {
	if cur := reg.Get(name); cur != nil {
		return fmt.Errorf("CmdRegister.Add: command %s already exists in register", name)
	}
	reg.Cmds[name] = cmd
	return nil
}

func tryConvert(ttype reflect.Type, str string) (reflect.Value, error) {
	if ttype.Kind() == reflect.String {
		return reflect.ValueOf(str), nil
	}
	/* 
	 * from https://stackoverflow.com/questions/39891689/how-to-convert-a-string-value-to-the-correct-reflect-kind-in-go,
	 * my original prototype was a huge swicth
	 */
	val := reflect.Zero(ttype)
	err := json.Unmarshal([]byte(str), val.Addr().Interface())
	return val, err
}
