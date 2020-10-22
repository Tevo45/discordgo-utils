package dgutils

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"strconv"

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
	channelType		 = reflect.TypeOf(&discordgo.Channel{})
	userType		 = reflect.TypeOf(&discordgo.User{})
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
 * FIXME
 * verify if we recover() everywhere a function can panic
 * this is all really messy
 *
 * TODO
 * have errors be their own type, so it's easier to handle
 * allow arrays as the last parameter of a command function
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

func (cmd *Cmd) Invoke(s *discordgo.Session, m *discordgo.MessageCreate, args []string) (err error) {
	/* Literally copy-pasted, but it needs to be a closure so err is in scope */
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("Invoke: %v", e)
		}
	}()

	if len(args) != len(cmd.paramTypes) {
		err = fmt.Errorf("Cmd.Invoke: expected %d arguments, but got %d", len(args), len(cmd.paramTypes))
		return
	}

	var vals []reflect.Value
	vals = append(vals, reflect.ValueOf(s), reflect.ValueOf(m))
	for c := 0; c < len(args); c++ {
		/* Need to declare this manually, := shadows err on the tryConvert call */
		var val reflect.Value

		expect := cmd.paramTypes[c]
		val, err = tryConvert(s, expect, args[c])

		if err != nil {
			return
		}

		vals = append(vals, val)
	}
	reflect.ValueOf(cmd.fn).Call(vals)
	return
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
		return fmt.Errorf("%s doesn't exist in register", name)
	}
	if cmd := reg.Get(name); cmd != nil {
		return fmt.Errorf("%s already exists in register", name)
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
			args := strings.Split(msg.Content, " ")	/* FIXME this breaks args with spaces */
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

/* TODO Support for discordgo types (members, channels, etc) */
func tryConvert(s *discordgo.Session, ttype reflect.Type, str string) (val reflect.Value, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("tryConvert: %v", e)
		}
	}()
	switch ttype.Kind() {
	case reflect.String:
		val = reflect.ValueOf(str)
	case reflect.Ptr:
		/* 
		 * For those, we first consider the string as a mention
		 * failing that, we look it up as an id, and if it fails,
		 * we give up
		 * We could try considering it as a name and looking it up,
		 * not sure if it's worth the effort
		 */
		switch underlying := ttype.Elem(); ttype {
		/* FIXME lots of repeated, really similar code */
		case channelType:
			var chann *discordgo.Channel
			var id uint64
			fmt.Sscanf(str, "<#%d>", &id)
			chann, _ = s.Channel(strconv.FormatUint(id, 10))
			if chann == nil {
				chann, _ = s.Channel(str)
			}
			if chann == nil {
				err = errors.New("tryConvert: cannot parse channel")
			} else {
				val = reflect.ValueOf(chann)
			}
		case userType:
			var user *discordgo.User
			var id uint64
			fmt.Sscanf(str, "<@%d>", &id)
			user, _ = s.User(strconv.FormatUint(id, 10))
			if user == nil {
				user, _ = s.User(str)
			}
			if user == nil {
				err = errors.New("tryConvert: cannot parse user")
			} else {
				val = reflect.ValueOf(user)
			}
		default:
			err = fmt.Errorf("tryConvert: can't unmarshal pointer to %s", underlying)
		}
	default:
		/*
		 * from https://stackoverflow.com/questions/39891689/how-to-convert-a-string-value-to-the-correct-reflect-kind-in-go,
		 * my original prototype was a huge swich for every type
		 */
		val = reflect.New(ttype)
		err = json.Unmarshal([]byte(str), val.Interface())
		if err == nil {
			val = val.Elem()
		}
	}
	return
}
