package mova

import (
	"errors"
	"fmt"
	"io"
	"maps"
)

type Registry struct {
	Triggers map[string]TriggerSpec
	Actions  map[string]ActionSpec
}

type TriggerSpec struct {
	Name    string
	Outputs map[string]ValueType // expected output name -> type
}

type ActionSpec struct {
	Name     string
	Inputs   map[string]ValueType // expected input name -> type
	Function func(map[string]any) // executed with resolved inputs
}

type CompiledMachine struct {
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

func (cond Condition) Test(name string, inputs map[string]any) bool {
	if cond.TriggerName != name {
		return false
	}
	for name, value := range cond.Value {
		expect, ok := inputs[name]
		if !ok {
			return false
		}
		if value != expect {
			return false
		}
	}
	return true
}

type CompiledTrigger struct {
	cond      []Condition
	datatypes map[string]ValueType
	actions   []Action
}

func (trg CompiledTrigger) Test(name string, inputs map[string]any) bool {
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

func BuildMachine(ast *File, reg *Registry, constants map[string]any) (*CompiledMachine, error) {
	var m CompiledMachine
	m.constants = make(map[string]Value)
	for name, value := range constants {
		m.constants[name] = &ConstantValue{value}
	}
	m.states = make(map[string]*CompiledState)
	for _, entry := range ast.Entries {
		if err := entry.EvalToplevel(&m, reg); err != nil {
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
	eval := make(map[string]any)
	for name, value := range ctx {
		var err error
		eval[name], err = value.EvalValue(ctx)
		if err != nil {
			return err
		}
	}
	for _, action := range actions {
		if err := action(m, eval); err != nil {
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

func fitType(v any) (Value, error) {
	switch v := v.(type) {
	case string:
		return &StringValue{v}, nil
	case int:
		return &IntValue{int64(v)}, nil
	case int8:
		return &IntValue{int64(v)}, nil
	case int16:
		return &IntValue{int64(v)}, nil
	case int32:
		return &IntValue{int64(v)}, nil
	case int64:
		return &IntValue{v}, nil
	case uint:
		return &IntValue{int64(v)}, nil
	case uint8:
		return &IntValue{int64(v)}, nil
	case uint16:
		return &IntValue{int64(v)}, nil
	case uint32:
		return &IntValue{int64(v)}, nil
	case uint64:
		return &IntValue{int64(v)}, nil
	case float32:
		return &FloatValue{float64(v)}, nil
	case float64:
		return &FloatValue{v}, nil
	case bool:
		return &BoolValue{v}, nil
	case fmt.Stringer:
		return &StringValue{v.String()}, nil
	}
	return nil, fmt.Errorf("statemachine cannot accept value of type %T", v)
}

func (m *StateMachine) Emit(name string, inputs map[string]any) error {
	for _, trg := range m.current.Triggers {
		if !trg.Test(name, inputs) {
			continue
		}

		ctx := maps.Clone(m.constants)
		for name, argtype := range trg.datatypes {
			v, ok := inputs[name]
			if !ok {
				return fmt.Errorf("missing event-data %q", name)
			}
			if argtype != ValueConstant {
				newvalue, err := fitType(v)
				if err != nil {
					return err
				}
				valuetype, err := newvalue.EvalType(m.constants)
				if err != nil {
					return err
				}
				if valuetype != argtype {
					return fmt.Errorf("type mismatch for event-data %q: expected %v, got %v", name, argtype, valuetype)
				}
				ctx[name] = newvalue
			} else {
				ctx[name] = &ConstantValue{v}
			}
		}
		return m.batch(trg.actions, ctx)
	}
	return io.EOF
}
