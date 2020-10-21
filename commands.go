package dgutils

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type Cmd struct {
	Help       string
	fn         interface{}
	paramTypes []reflect.Type
}

type CmdRegister struct {
	Cmds    map[string]*Cmd
	Aliases map[string]string
}

type CmdErrorHandler func(*discordgo.Session, *discordgo.MessageCreate, error)

var (
	sessionType      = reflect.TypeOf(&discordgo.Session{})
	messageEventType = reflect.TypeOf(&discordgo.MessageCreate{})
	illegalKinds     = map[reflect.Kind]bool{
		reflect.Invalid:       true,
		reflect.Uintptr:       true,
		reflect.Array:         true,
		reflect.Chan:          true,
		reflect.Func:          true,
		reflect.Interface:     true,
		reflect.Map:           true,
		reflect.Slice:         true,
		reflect.Struct:        true,
		reflect.UnsafePointer: true,
	}
)

/*
 * TODO
 * Handle()
 */

func Command(fn interface{}, help string) (*Cmd, error) {
	val := reflect.ValueOf(fn)
	if kind := val.Kind(); kind != reflect.Func {
		return nil, fmt.Errorf("Command: expected fn of kind Func, got %s", kind)
	}
	ttype := val.Type()
	if ttype.NumIn() < 2 {
		return nil, errors.New("Command: not enough arguments")
	}
	/* Can we compare pointer types like that? */
	if first := ttype.In(0); first != sessionType {
		return nil, errors.New("Command: fn's first argument is not a pointer to a discordgo.Session")
	}
	if snd := ttype.In(1); snd != messageEventType {
		return nil, errors.New("Command: fn's second argument is not a pointer to a discordgo.MessageCreate")
	}
	var params []reflect.Type
	for c := 2; c < ttype.NumIn(); c++ {
		param := ttype.In(c)
		if kind := param.Kind(); illegalKinds[kind] {
			return nil, fmt.Errorf("Command: argument of kind %s not supported", kind)
		}
		params = append(params, param)
	}
	return &Cmd{Help: help, fn: fn, paramTypes: params}, nil
}

/* FIXME the name */
func MustCommand(fn interface{}, help string) *Cmd {
	cmd, err := Command(fn, help)
	if err != nil {
		panic(err)
	}
	return cmd
}

func (cmd *Cmd) Invoke(s *discordgo.Session, m *discordgo.MessageCreate, args []string) error {
	if len(args) != len(cmd.paramTypes) {
		return errors.New("Cmd.Invoke: argument-parameter count mismatch")
	}
	var vals []reflect.Value
	vals = append(vals, reflect.ValueOf(s), reflect.ValueOf(m))
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
	canon := reg.Aliases[name]
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

func (reg *CmdRegister) Alias(name string, dest string) error {
	if cmd := reg.Get(dest); cmd == nil {
		return fmt.Errorf("%s doesn't exist in register")
	}
	if cmd := reg.Get(name); cmd != nil {
		return fmt.Errorf("%s already exists in register")
	}
	reg.Aliases[name] = dest
	return nil
}

func (reg *CmdRegister) Handler(
	pfx string,
	errHandler CmdErrorHandler,
) func(*discordgo.Session, *discordgo.MessageCreate) {
	return func(s *discordgo.Session, msg *discordgo.MessageCreate) {
		if msg.Author.ID == s.State.User.ID {
			return
		}
		if strings.HasPrefix(msg.Content, pfx) {
			args := strings.Split(msg.Content, " ")
			str := args[0]
			str = strings.Replace(str, pfx, "", 1)
			cmd := reg.Get(str)
			if cmd != nil {
				err := cmd.Invoke(s, msg, args[1:])
				if err != nil && errHandler != nil {
					errHandler(s, msg, err)
				}
			}
		}
	}
}

func Register() *CmdRegister {
	return &CmdRegister{
		Cmds:    map[string]*Cmd{},
		Aliases: map[string]string{},
	}
}

func tryConvert(ttype reflect.Type, str string) (val reflect.Value, err error) {
	defer func() {
		err = recover()
	}()
	if ttype.Kind() == reflect.String {
		val = reflect.ValueOf(str)
		return
	}
	/*
	 * from https://stackoverflow.com/questions/39891689/how-to-convert-a-string-value-to-the-correct-reflect-kind-in-go,
	 * my original prototype was a huge swich
	 */
	val = reflect.Zero(ttype)
	err = json.Unmarshal([]byte(str), val.Addr().Interface())
	return
}
