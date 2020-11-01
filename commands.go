package dgutils

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type Cmd interface {
	Invoke(s *discordgo.Session, m *discordgo.MessageCreate, args []string) error
	ErrorHandler() CmdErrorHandler
}

//
// Command backed by a Go function. Arguments are reflected and automatically
// converted at runtime.
// Predicate is an optional CmdPredicate struct describing in which conditions
// the command may be executed.
// ErrHandler is an optional error handling function that may be invoked in
// case the command fails to be invoked.
//
type FnCmd struct {
	Help       string
	fn         interface{}
	Predicate  CmdPredicate
	ErrHandler CmdErrorHandler
	paramTypes []reflect.Type
}

type CmdRegister struct {
	Cmds    map[string]Cmd
	Aliases map[string]string
}

//
// Describes in which condition a command may be executed.
// Permissions is a bitfield describing necessary user premissions for
// invoking the command.
// Custom is a function that can be used to check for logic not directly
// implemented by a predicate.
//
type CmdPredicate struct {
	Permissions int
	Custom      CmdPredicateFunc
}

type CmdErrorHandler func(*discordgo.Session, *discordgo.MessageCreate, error)
type CmdPredicateFunc func(*discordgo.Session, *discordgo.MessageCreate, CmdPredicate) bool

var (
	sessionType      = reflect.TypeOf(&discordgo.Session{})
	messageEventType = reflect.TypeOf(&discordgo.MessageCreate{})
	channelType      = reflect.TypeOf(&discordgo.Channel{})
	userType         = reflect.TypeOf(&discordgo.User{})
	illegalKinds     = map[reflect.Kind]bool{
		reflect.Invalid:       true,
		reflect.Uintptr:       true,
		reflect.Array:         true,
		reflect.Chan:          true,
		reflect.Func:          true,
		reflect.Interface:     true,
		reflect.Map:           true,
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
 * support variadic functions
 * make cmd a interface
 */

//
// Creates a command from a given function fn, with help as the help string,
// and errHandler as an optional error handler.
//
// fn must have a *discordgo.Session as the first parameter, and *discordgo.MessageCreate
// as the second. Later parameters are taken as command parameters, and are converted
// automatically upon invocation. Valid parameter types include integer and float types,
// string, bool and pointers to some discordgo types (User, Channel, Role and Member),
// Arrays of supported types are accepted as the last argument of a function, and
// will behave as if the command was a variadic function.
//
func Command(fn interface{}, help string, errHandler CmdErrorHandler) (*FnCmd, error) {
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
		} else if kind == reflect.Slice {
			if c != ttype.NumIn()-1 {
				return nil, errors.New("Command: slice can only be the last argument in a function")
			}
			if illegalKinds[param.Elem().Kind()] {
				return nil, fmt.Errorf("Command: argument of kind %s not supported", kind)
			}
		}
		params = append(params, param)
	}
	return &FnCmd{Help: help, fn: fn, paramTypes: params, ErrHandler: errHandler}, nil
}

//
// Same as Command, but also takes a predicate struct. Predicates may be used to limit
// commands to users with certain permission levels, or perform additional validation
// before executing a command.
//
func PredicatedCommand(
	fn interface{},
	help string,
	errHandler CmdErrorHandler,
	predicate CmdPredicate,
) (cmd *FnCmd, err error) {
	cmd, err = Command(fn, help, errHandler)
	if cmd != nil {
		cmd.Predicate = predicate
	}
	return
}

//
// Same as Command, but it panics if an error is encountered
//
func MustCommand(fn interface{}, help string, errHandler CmdErrorHandler) *FnCmd {
	cmd, err := Command(fn, help, errHandler)
	if err != nil {
		panic(err)
	}
	return cmd
}

//
// Same as PredicatedCommand, but it panics if an error is encountered
//
func MustPredicatedCommand(
	fn interface{},
	help string,
	errHandler CmdErrorHandler,
	predicate CmdPredicate,
) *FnCmd {
	cmd, err := PredicatedCommand(fn, help, errHandler, predicate)
	if err != nil {
		panic(err)
	}
	return cmd
}

//
// Verifies whether the message m satisfies the predicate
//
func (p CmdPredicate) Validate(s *discordgo.Session, m *discordgo.MessageCreate) bool {
	if p.Permissions != 0 {
		owner, _ := IsOwner(s, m.GuildID, m.Author.ID)
		perm, _ := MemberHasPermissions(s, m.GuildID, m.Author.ID, p.Permissions)
		if !owner && !perm {
			return false
		}
	}
	if p.Custom != nil && p.Custom(s, m, p) {
		return false
	}
	return true
}

func (cmd *FnCmd) ErrorHandler() CmdErrorHandler {
	return cmd.ErrHandler
}

//
// Invokes the command based on message creation event m with arguments args.
// Arguments are automatically parsed to their required type; an error is returned
// if it can't be done. args should not contain the command name as it's first member,
// but it might be empty if it is required.
//
func (cmd *FnCmd) Invoke(s *discordgo.Session, m *discordgo.MessageCreate, args []string) (err error) {
	/* Literally copy-pasted, but it needs to be a closure so err is in scope */
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("Cmd.Invoke: %v", e)
		}
	}()

	if !cmd.Predicate.Validate(s, m) {
		err = AccessDenied{}
		return
	}

	expectLen := len(cmd.paramTypes)
	actualLen := len(args)
	sliceReceiver := cmd.paramTypes[expectLen-1].Kind() == reflect.Slice
	if actualLen < expectLen || (!sliceReceiver && actualLen > expectLen) {
		err = ArgCountMismatch{expectLen, actualLen}
		return
	}

	var vals []reflect.Value
	vals = append(vals, reflect.ValueOf(s), reflect.ValueOf(m))
	for c := 0; c < len(args); c++ {
		/* Need to declare this manually, := shadows err on the tryConvert call */
		var val reflect.Value

		expect := cmd.paramTypes[c]
		if expect.Kind() == reflect.Slice {
			sliceType := expect.Elem()
			slice := reflect.New(expect).Elem()
			for ; c < len(args); c++ {
				val, err = tryConvert(s, sliceType, args[c])
				if err != nil {
					return
				}
				slice = reflect.Append(slice, val)
			}
			val = slice
		} else {
			val, err = tryConvert(s, expect, args[c])
		}

		if err != nil {
			return
		}

		vals = append(vals, val)
	}
	reflect.ValueOf(cmd.fn).Call(vals)
	return
}

//
// Returns the canonical name of a command
//
func (reg *CmdRegister) Canon(name string) string {
	canon := reg.Aliases[name]
	if canon != "" {
		return canon
	}
	return name
}

//
// Returns a commend in the register, or nil if the command doesn't exist
// name might be a canon name or an alias
//
func (reg *CmdRegister) Get(name string) Cmd {
	return reg.Cmds[reg.Canon(name)]
}

func (reg *CmdRegister) Add(name string, cmd Cmd) error {
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

//
// Handles commands in the context of this register
// pfx represents a prefix string for prefixed commands
// errHandler is an optional error handler. If non-nil, it will be called when a command
// returns an error when executing. It can be overriden on a per-command basis
//
func (reg *CmdRegister) Handle(
	s *discordgo.Session,
	msg *discordgo.MessageCreate,
	pfx string,
	errHandler CmdErrorHandler,
) {
	if msg.Author.ID == s.State.User.ID {
		return
	}
	if strings.HasPrefix(msg.Content, pfx) {
		args := strings.Split(msg.Content, " ") /* FIXME this breaks args with spaces */
		str := args[0]
		str = strings.Replace(str, pfx, "", 1)
		cmd := reg.Get(str)
		if cmd != nil {
			err := cmd.Invoke(s, msg, args[1:])
			handler := errHandler
			if cmdHandler := cmd.ErrorHandler(); cmdHandler != nil {
				handler = cmdHandler
			}
			if err != nil && handler != nil {
				handler(s, msg, err)
			}
		}
	}
}

//
// Returns a handler function, suitable to be used with discordgo.Session.AddHandler
// pfx represents a prefix string for prefixed commands
// errHandler is an optional error handler. If non-nil, it will be called when a command
// returns an error when executing. It can be overriden on a per-command basis
//
func (reg *CmdRegister) Handler(
	pfx string,
	errHandler CmdErrorHandler,
) func(*discordgo.Session, *discordgo.MessageCreate) {
	return func(s *discordgo.Session, msg *discordgo.MessageCreate) {
		reg.Handle(s, msg, pfx, errHandler)
	}
}

//
// Creates an empty command register
//
func Register() *CmdRegister {
	return &CmdRegister{
		Cmds:    map[string]Cmd{},
		Aliases: map[string]string{},
	}
}

//
// Attempts to parse str into the required type ttype, errors if it can't be done
//
func tryConvert(s *discordgo.Session, ttype reflect.Type, str string) (val reflect.Value, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = UnmarshalError{fmt.Errorf("tryConvert: %v", e)}
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
				err = UnmarshalError{errors.New("tryConvert: cannot parse channel")}
			} else {
				val = reflect.ValueOf(chann)
			}
		case userType:
			var user *discordgo.User
			var id uint64
			fmt.Sscanf(str, "<@!%d>", &id)
			user, _ = s.User(strconv.FormatUint(id, 10))
			if user == nil {
				user, _ = s.User(str)
			}
			if user == nil {
				err = UnmarshalError{errors.New("tryConvert: cannot parse user")}
			} else {
				val = reflect.ValueOf(user)
			}
		default:
			err = UnmarshalError{
				fmt.Errorf("tryConvert: can't unmarshal pointer to %s", underlying),
			}
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
		} else {
			err = UnmarshalError{err}
		}
	}
	return
}
