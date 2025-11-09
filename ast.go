package mova

import (
	"errors"
	"fmt"
	"log"
	"maps"
)

type Action func(m *StateMachine, input map[string]any) error

type Statement interface {
	CheckType(map[string]Value, *Registry) error
	Execute(*Registry) Action
}

type Entry interface {
	EvalToplevel(*CompiledMachine, *Registry) error
}

type File struct {
	Entries []Entry
}

type State struct {
	Name     string
	Init     []Statement
	Triggers []Trigger
}

func (trg *Trigger) evalTrigger(state string, index int, m *CompiledMachine, reg *Registry) (CompiledTrigger, error) {
	var out CompiledTrigger

	out.datatypes = make(map[string]ValueType)
	local := maps.Clone(m.constants)

	for condidx, c := range trg.Cond {
		spec, ok := reg.Triggers[c.Name]
		if !ok {
			return out, fmt.Errorf("in trigger %s#%d: unspecified trigger %q", state, index, c.Name)
		}

		var cond = Condition{
			TriggerName: c.Name,
			Value:       make(map[string]any),
		}

		prevkeys := make(map[string]bool)
		for name := range out.datatypes {
			prevkeys[name] = false
		}
		for _, param := range c.Params {
			argtype, ok := spec.Outputs[param.Key]
			if !ok {
				return out, fmt.Errorf("in trigger %s#%d: unspecified event-data %q for trigger %s", state, index, param.Key, c.Name)
			}
			if param.Value != nil {
				fmt.Printf("[%s] %v\n", param.Key, param.Value)
				condtype, err := param.Value.EvalType(m.constants)
				if err != nil {
					return out, fmt.Errorf("in trigger %s#%d: cannot determine type of variable for event-data %q: %w", state, index, param.Key, err)
				}
				if condtype != argtype {
					return out, fmt.Errorf("in trigger %s#%d: type mismatch for event-data %q: expected %v, got %v", state, index, param.Key, argtype, condtype)
				}
				cond.Value[param.Key], err = param.Value.EvalValue(m.constants)
				if err != nil {
					return out, fmt.Errorf("in trigger %s#%d: cannot evaluate conditional value for event-data %q: %w", state, index, param.Key, err)
				}
			}
			prevkeys[param.Key] = true
			if prevtype, ok := out.datatypes[param.Key]; ok {
				if prevtype != argtype {
					return out, fmt.Errorf("in trigger %s#%d: type mismatch for event-data %q: unable to redefine to %v (previously %v)", state, index, param.Key, argtype, prevtype)
				}
			} else {
				out.datatypes[param.Key] = argtype
				local[param.Key] = &TypeDummyValue{argtype}
			}
		}
		for name, mentioned := range prevkeys {
			if mentioned {
				continue
			}
			log.Printf("in trigger %s#%d: dropping previous event-data %q: not mentioned in condition #%d\n", state, index, name, condidx)
			delete(out.datatypes, name)
			delete(local, name)
		}
		out.cond = append(out.cond, cond)
	}
	fmt.Printf("in trigger %s#%d: types %+v\n", state, index, out.datatypes)
	for _, stmt := range trg.Actions {
		if err := stmt.CheckType(local, reg); err != nil {
			return out, err
		}
		out.actions = append(out.actions, stmt.Execute(reg))
	}
	return out, nil
}

func (st *State) EvalToplevel(m *CompiledMachine, reg *Registry) error {
	var outstate CompiledState
	for _, stmt := range st.Init {
		if err := stmt.CheckType(m.constants, reg); err != nil {
			return err
		}
		outstate.Init = append(outstate.Init, stmt.Execute(reg))
	}
	for i, trg := range st.Triggers {
		ctrg, err := trg.evalTrigger(st.Name, i, m, reg)
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

func (ss *SetStmt) EvalToplevel(m *CompiledMachine, reg *Registry) error {
	m.constants[ss.Key] = ss.Value
	return nil
}

type MoveStmt struct {
	Dest string
}

func (ms *MoveStmt) CheckType(_ map[string]Value, reg *Registry) error {
	return nil
}

func (ms *MoveStmt) Execute(*Registry) Action {
	return func(m *StateMachine, input map[string]any) error {
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
	Args []Arg
}

func (c *Call) CheckType(ctx map[string]Value, reg *Registry) error {
	spec, ok := reg.Actions[c.Name]
	if !ok {
		return fmt.Errorf("unspecified action %q", c.Name)
	}
	for _, param := range c.Args {
		argtype, ok := spec.Inputs[param.Key]
		if !ok {
			return fmt.Errorf("unspecified argument %q for action %s", param.Key, c.Name)
		}
		valuetype, err := param.Value.EvalType(ctx)
		if err != nil {
			return fmt.Errorf("cannot determine type of variable for argument %q: %w", param.Key, err)
		}
		if valuetype != argtype {
			return fmt.Errorf("type mismatch for argument %s.%s: expected %v, got %v", c.Name, param.Key, argtype, valuetype)
		}
	}
	return nil
}

func (c *Call) Execute(reg *Registry) Action {
	spec := reg.Actions[c.Name]
	return func(m *StateMachine, ctx map[string]any) error {
		spec.Function(ctx)
		return nil
	}
}

type Arg struct {
	Key   string
	Value Value
}

type ValueType int

const (
	ValueString ValueType = iota
	ValueInt
	ValueFloat
	ValueBool
	ValueConstant
)

func (i ValueType) String() string {
	switch i {
	case ValueString:
		return "string"
	case ValueInt:
		return "integer"
	case ValueFloat:
		return "float"
	case ValueBool:
		return "boolean"
	case ValueConstant:
		return "untyped-constant"
	}
	return fmt.Sprintf("ValueType(%d)", i)
}

type Value interface {
	EvalValue(ctx map[string]Value) (any, error)
	EvalType(ctx map[string]Value) (ValueType, error)
}

type StringValue struct {
	Value string
}

func (v *StringValue) EvalValue(ctx map[string]Value) (any, error) {
	return v.Value, nil
}

func (v *StringValue) EvalType(ctx map[string]Value) (ValueType, error) {
	return ValueString, nil
}

type IntValue struct {
	Value int64
}

func (v *IntValue) EvalValue(ctx map[string]Value) (any, error) {
	return v.Value, nil
}

func (v *IntValue) EvalType(ctx map[string]Value) (ValueType, error) {
	return ValueInt, nil
}

type FloatValue struct {
	Value float64
}

func (v *FloatValue) EvalValue(ctx map[string]Value) (any, error) {
	return v.Value, nil
}

func (v *FloatValue) EvalType(ctx map[string]Value) (ValueType, error) {
	return ValueFloat, nil
}

type BoolValue struct {
	Value bool
}

func (v *BoolValue) EvalValue(ctx map[string]Value) (any, error) {
	return v.Value, nil
}

func (v *BoolValue) EvalType(ctx map[string]Value) (ValueType, error) {
	return ValueBool, nil
}

type ConstantValue struct {
	Value any
}

func (v *ConstantValue) EvalValue(ctx map[string]Value) (any, error) {
	return v.Value, nil
}

func (v *ConstantValue) EvalType(ctx map[string]Value) (ValueType, error) {
	return ValueConstant, nil
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

func (v *ReferenceValue) EvalType(ctx map[string]Value) (ValueType, error) {
	ref, ok := ctx[v.Ref]
	if !ok {
		return 0, fmt.Errorf("undefined variable %q", v.Ref)
	}
	return ref.EvalType(ctx)
}

var ErrDummyNotEvaluable = errors.New("Dummy Value not evaluable.")

type TypeDummyValue struct {
	typ ValueType
}

func (v *TypeDummyValue) EvalValue(ctx map[string]Value) (any, error) {
	return nil, ErrDummyNotEvaluable
}

func (v *TypeDummyValue) EvalType(ctx map[string]Value) (ValueType, error) {
	return v.typ, nil
}
