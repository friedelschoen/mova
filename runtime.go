package mova

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
)

func getTypeField(base reflect.Type, name string) int {
	for i := range base.NumField() {
		field := base.Field(i)
		if field.Name == name || field.Tag.Get("mova") == name {
			return i
		}
	}
	return -1
}

type Registry struct {
	triggers map[string]reflect.Type
	actions  map[string]ActionSpec
}

func NewTrigger[T any](r *Registry, name string) {
	if r.triggers == nil {
		r.triggers = make(map[string]reflect.Type)
	}
	r.triggers[name] = reflect.TypeFor[T]()
}

func NewAction(r *Registry, name string, args []string, fn any) {
	val := reflect.ValueOf(fn)
	if val.Type().NumIn() != len(args) {
		panic(fmt.Errorf("action has %d arguments, %d expected", val.Type().NumIn(), len(args)))
	}
	if r.actions == nil {
		r.actions = make(map[string]ActionSpec)
	}
	r.actions[name] = ActionSpec{
		Inputs:   args,
		Function: val,
	}
}

type ActionSpec struct {
	Inputs   []string      // expected input name -> type
	Function reflect.Value // executed with resolved inputs
}

type CompiledMachine struct {
	reg        *Registry
	constants  map[string]Value
	firstState string
	states     map[string]*CompiledState
}

type StateMachine struct {
	CompiledMachine
	current *CompiledState
}

type Condition struct {
	TriggerName string
	Value       map[string]any
}

func (cond Condition) Test(name string, inputs reflect.Value) bool {
	if cond.TriggerName != name {
		return false
	}
	inputtypes := inputs.Type()
	for name, value := range cond.Value {
		i := getTypeField(inputtypes, name)
		if i == -1 {
			return false
		}
		if value != inputs.Field(i).Interface() {
			return false
		}
	}
	return true
}

type CompiledTrigger struct {
	cond      []Condition
	datatypes []string
	actions   []Action
}

func (trg CompiledTrigger) Test(name string, inputs reflect.Value) bool {
	for _, cond := range trg.cond {
		if cond.Test(name, inputs) {
			return true
		}
	}
	return false
}

type CompiledState struct {
	Init     []Action
	Triggers []CompiledTrigger
}

var ErrEmptyMachine = errors.New("empty state machine")

func BuildMachine(filename string, r io.Reader, reg *Registry, constants map[string]any) (*CompiledMachine, error) {
	p := parser{lexer: newLexer(r, rules), filename: filename}
	ast, err := p.ParseFile()
	if err != nil {
		return nil, err
	}

	var m CompiledMachine
	m.reg = reg
	m.constants = make(map[string]Value)
	for name, value := range constants {
		m.constants[name] = &ConstValue{value}
	}
	m.states = make(map[string]*CompiledState)
	for _, entry := range ast.Entries {
		if err := entry.EvalToplevel(&m); err != nil {
			return nil, err
		}
	}
	if len(m.states) == 0 {
		return nil, ErrEmptyMachine
	}
	return &m, nil
}

func (cm *CompiledMachine) New() (*StateMachine, error) {
	var m StateMachine
	m.CompiledMachine = *cm
	err := m.move(m.firstState)
	return &m, err
}

func (m *StateMachine) batch(actions []Action, ctx map[string]Value) error {
	for _, action := range actions {
		if err := action(m, ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m *StateMachine) move(dest string) error {
	newstate, ok := m.states[dest]
	if !ok {
		return fmt.Errorf("unknown state %q", dest)
	}
	m.current = newstate
	return m.batch(newstate.Init, m.constants)
}

func (m *StateMachine) Emit(name string, v any) error {
	rval := reflect.ValueOf(v)
	etyp, ok := m.reg.triggers[name]
	if !ok {
		return fmt.Errorf("unspecified event %q", name)
	}
	if etyp != rval.Type() {
		return fmt.Errorf("invalid type for event %q, expected %v got %v", name, etyp, rval.Type())
	}
	for _, trg := range m.current.Triggers {
		if !trg.Test(name, rval) {
			continue
		}

		ctx := maps.Clone(m.constants)
		for _, name := range trg.datatypes {
			i := getTypeField(rval.Type(), name)
			if i == -1 {
				continue
			}
			ctx[name] = &ConstValue{rval.Field(i).Interface()}
		}
		return m.batch(trg.actions, ctx)
	}
	return io.EOF
}
