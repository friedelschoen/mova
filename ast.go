package mova

import (
	"errors"
	"fmt"
	"log"
	"maps"
	"reflect"
	"slices"
)

type Action func(m *StateMachine, input map[string]Value) error

type Statement interface {
	CheckType(map[string]Value, *CompiledMachine) error
	Execute(*CompiledMachine) Action
}

type Entry interface {
	EvalToplevel(*CompiledMachine) error
}

type File struct {
	Entries []Entry
}

type State struct {
	Name     string
	Init     []Statement
	Triggers []Trigger
}

func (trg *Trigger) evalTrigger(state string, index int, m *CompiledMachine) (CompiledTrigger, error) {
	var out CompiledTrigger

	datatypes := make(map[string]reflect.Type)
	local := maps.Clone(m.constants)

	for condidx, c := range trg.Cond {
		spec, ok := m.reg.triggers[c.Name]
		if !ok {
			return out, fmt.Errorf("in trigger %s#%d: unspecified trigger %q", state, index, c.Name)
		}

		var cond = Condition{
			TriggerName: c.Name,
			Value:       make(map[string]any),
		}

		prevkeys := make(map[string]bool)
		for name := range datatypes {
			prevkeys[name] = false
		}
		for _, param := range c.Params {
			i := getTypeField(spec, param.Key)
			if i == -1 {
				return out, fmt.Errorf("in trigger %s#%d: unspecified event-data %q for trigger %s", state, index, param.Key, c.Name)
			}
			argtype := spec.Field(i).Type
			if param.Value != nil {
				condtype, err := param.Value.EvalType(m.constants)
				if err != nil {
					return out, fmt.Errorf("in trigger %s#%d: cannot determine type of variable for event-data %q: %w", state, index, param.Key, err)
				}
				if condtype != argtype {
					return out, fmt.Errorf("in trigger %s#%d: type mismatch for event-data %q: expected %v, got %v", state, index, param.Key, argtype.Name(), condtype.Name())
				}
				cond.Value[param.Key], err = param.Value.EvalValue(m.constants)
				if err != nil {
					return out, fmt.Errorf("in trigger %s#%d: cannot evaluate conditional value for event-data %q: %w", state, index, param.Key, err)
				}
			}
			prevkeys[param.Key] = true
			if prevtype, ok := datatypes[param.Key]; ok {
				if prevtype != argtype {
					return out, fmt.Errorf("in trigger %s#%d: type mismatch for event-data %q: unable to redefine to %v (previously %v)", state, index, param.Key, argtype, prevtype)
				}
			} else {
				datatypes[param.Key] = argtype
				local[param.Key] = &TypeDummyValue{argtype}
			}
		}
		for name, mentioned := range prevkeys {
			if mentioned {
				continue
			}
			log.Printf("in trigger %s#%d: dropping previous event-data %q: not mentioned in condition #%d\n", state, index, name, condidx)
			delete(datatypes, name)
			delete(local, name)
		}
		out.cond = append(out.cond, cond)
	}
	for _, stmt := range trg.Actions {
		if err := stmt.CheckType(local, m); err != nil {
			return out, err
		}
		out.actions = append(out.actions, stmt.Execute(m))
	}
	out.datatypes = slices.Collect(maps.Keys(datatypes))
	return out, nil
}

func (st *State) EvalToplevel(m *CompiledMachine) error {
	var outstate CompiledState
	for _, stmt := range st.Init {
		if err := stmt.CheckType(m.constants, m); err != nil {
			return err
		}
		outstate.Init = append(outstate.Init, stmt.Execute(m))
	}
	for i, trg := range st.Triggers {
		ctrg, err := trg.evalTrigger(st.Name, i, m)
		if err != nil {
			return err
		}
		outstate.Triggers = append(outstate.Triggers, ctrg)
	}
	m.states[st.Name] = &outstate
	if m.firstState == "" {
		m.firstState = st.Name
	}
	return nil
}

type SetStmt struct {
	Key   string
	Value Value
}

func (ss *SetStmt) EvalToplevel(m *CompiledMachine) error {
	m.constants[ss.Key] = ss.Value
	return nil
}

type MoveStmt struct {
	Dest string
}

func (ms *MoveStmt) CheckType(_ map[string]Value, m *CompiledMachine) error {
	return nil
}

func (ms *MoveStmt) Execute(*CompiledMachine) Action {
	return func(m *StateMachine, input map[string]Value) error {
		return m.move(ms.Dest)
	}
}

type TriggerCond struct {
	Name   string
	Params []Arg
}

type Trigger struct {
	Cond    []TriggerCond
	Actions []Statement
}

type Call struct {
	Name string
	Args map[string]Value
}

func (c *Call) CheckType(ctx map[string]Value, m *CompiledMachine) error {
	spec, ok := m.reg.actions[c.Name]
	if !ok {
		return fmt.Errorf("unspecified action %q", c.Name)
	}
	for key, value := range c.Args {
		i := slices.Index(spec.Inputs, key)
		if i == -1 {
			return fmt.Errorf("unspecified argument %q for action %s", key, c.Name)
		}
		argtype := spec.Function.Type().In(i)
		valuetype, err := value.EvalType(ctx)
		if err != nil {
			return fmt.Errorf("cannot determine type of variable for argument %q: %w", key, err)
		}
		if !valuetype.ConvertibleTo(argtype) && reflect.PointerTo(valuetype).ConvertibleTo(argtype) {
			return fmt.Errorf("type mismatch for argument %s.%s: expected %v, got %v", c.Name, key, argtype, valuetype)
		}
	}
	return nil
}

func (c *Call) Execute(m *CompiledMachine) Action {
	spec := m.reg.actions[c.Name]
	return func(m *StateMachine, ctx map[string]Value) error {
		ins := make([]reflect.Value, len(spec.Inputs))
		for i, name := range spec.Inputs {
			argtype := spec.Function.Type().In(i)
			v, ok := c.Args[name]
			if ok {
				eval, err := v.EvalValue(ctx)
				if err != nil {
					return err
				}
				if evt := reflect.ValueOf(eval); evt.CanConvert(argtype) {
					ins[i] = evt.Convert(argtype)
				} else if evt := reflect.ValueOf(&eval); evt.CanConvert(argtype) {
					ins[i] = evt.Convert(argtype)
				} else {
					return fmt.Errorf("unable to convert argument %s.%s from %v to %v", c.Name, name, reflect.TypeOf(eval), argtype)
				}
			} else {
				ins[i] = reflect.Zero(spec.Function.Type().In(i))
			}
		}
		spec.Function.Call(ins)
		return nil
	}
}

type Arg struct {
	Key   string
	Value Value
}

type Value interface {
	EvalValue(ctx map[string]Value) (any, error)
	EvalType(ctx map[string]Value) (reflect.Type, error)
}

type ConstValue struct {
	Value any
}

func (v *ConstValue) EvalValue(ctx map[string]Value) (any, error) {
	return v.Value, nil
}

func (v *ConstValue) EvalType(ctx map[string]Value) (reflect.Type, error) {
	return reflect.TypeOf(v.Value), nil
}

type ReferenceValue struct {
	Ref string
}

func (v *ReferenceValue) EvalValue(ctx map[string]Value) (any, error) {
	ref, ok := ctx[v.Ref]
	if !ok {
		return nil, fmt.Errorf("undefined variable %q", v.Ref)
	}
	return ref.EvalValue(ctx)
}

func (v *ReferenceValue) EvalType(ctx map[string]Value) (reflect.Type, error) {
	ref, ok := ctx[v.Ref]
	if !ok {
		return nil, fmt.Errorf("undefined variable %q", v.Ref)
	}
	return ref.EvalType(ctx)
}

var ErrDummyNotEvaluable = errors.New("Dummy Value not evaluable.")

type TypeDummyValue struct {
	typ reflect.Type
}

func (v *TypeDummyValue) EvalValue(ctx map[string]Value) (any, error) {
	return nil, ErrDummyNotEvaluable
}

func (v *TypeDummyValue) EvalType(ctx map[string]Value) (reflect.Type, error) {
	return v.typ, nil
}
