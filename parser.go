package mova

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var rules = []rule{
	{"", regexp.MustCompile(`^[\s\t\r\n]+`)}, // whitespace
	{"", regexp.MustCompile(`^#[^\n]*`)},     // comment

	{"arrow", regexp.MustCompile(`^->`)},
	{"punct", regexp.MustCompile(`^[{}(),;=]`)},
	{"string", regexp.MustCompile(`^"(\\.|[^"\\])*"`)},
	{"float", regexp.MustCompile(`^[+-]?[0-9]+\.[0-9]*`)},
	{"int", regexp.MustCompile(`^[+-]?[0-9]+`)},
	{"bool", regexp.MustCompile(`^(true|false)\b`)},
	{"keyword", regexp.MustCompile(`^(state|on|move)\b`)},
	{"identifier", regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)},
}

type parser struct {
	*lexer
	filename string
}

func (p *parser) expect(name string) string {
	if p.Token != name {
		p.errUnexpected(name)
	}
	v := p.Value
	p.Next()
	return v
}

func (p *parser) expectValue(val string) {
	if p.Value != val {
		p.errUnexpected(strconv.Quote(val))
	}
	p.Next()
}

type ParseError struct {
	Filename             string
	Expected             []string
	Line, Offset, Length int
	Type, Value          string
}

func (perr *ParseError) Error() string {
	var exp strings.Builder
	if len(perr.Expected) == 0 {
		exp.WriteString("??")
	} else if len(perr.Expected) == 1 {
		exp.WriteString(perr.Expected[0])
	} else if len(perr.Expected) > 1 {
		for i, tok := range perr.Expected[:len(perr.Expected)-1] {
			if i > 0 {
				exp.WriteString(", ")
			}
			exp.WriteString(tok)
		}
		exp.WriteString(" or ")
		exp.WriteString(perr.Expected[len(perr.Expected)-1])
	}
	return fmt.Sprintf("%s:%d:%d-%d: expected %s, got %q", perr.Filename, perr.Line, perr.Offset, perr.Offset+perr.Length, exp.String(), perr.Value)
}

func (p *parser) errUnexpected(expected ...string) {
	err := &ParseError{
		Filename: p.filename,
		Expected: expected,
		Line:     p.Linenr,
		Offset:   p.Offset,
		Length:   p.Length,
		Type:     p.Token,
		Value:    p.Value,
	}
	panic(err)
}

// entry point
func (p *parser) ParseFile() (f *File, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("panic: %v", r)
			}
		}
	}()

	f = &File{}
	for p.Token != "EOF" {
		e := p.parseEntry()
		f.Entries = append(f.Entries, e)
	}
	p.expect("EOF")
	return f, nil
}

func (p *parser) parseEntry() Entry {
	if p.Value == "state" {
		st := p.parseState()
		p.expectValue(";")
		return st
	}
	if p.Token == "identifier" {
		key := p.expect("identifier")
		p.expectValue("=")
		val := p.parseValue()
		p.expectValue(";")
		return &SetStmt{Key: key, Value: val}
	}
	p.errUnexpected("identifier", "\"state\"")
	return nil
}

func (p *parser) parseState() *State {
	p.expectValue("state")
	name := p.expect("identifier")
	p.expectValue("{")
	var init []Statement
	if p.Value != "on" {
		init = append(init, p.parseAction())
		for p.Value == "," {
			p.Next()
			init = append(init, p.parseAction())
		}
		p.expectValue(";")
	}
	var triggers []Trigger
	for p.Value != "}" {
		triggers = append(triggers, p.parseTrigger())
	}
	p.expectValue("}")
	return &State{Name: name, Init: init, Triggers: triggers}
}

func (p *parser) parseTriggerCond() TriggerCond {
	name := p.expect("identifier")
	var params []Arg
	if p.Value == "(" {
		p.Next()
		for p.Value != ")" {
			params = append(params, p.parseParam())
			if p.Value != "," {
				break
			}
			p.Next() // skip comma
		}
		p.expectValue(")")
	}
	return TriggerCond{name, params}
}

func (p *parser) parseTrigger() Trigger {
	p.expectValue("on")
	var conds []TriggerCond
	conds = append(conds, p.parseTriggerCond())
	for p.Value == "," {
		conds = append(conds, p.parseTriggerCond())
	}
	p.expectValue("->")
	var actions []Statement
	actions = append(actions, p.parseAction())
	for p.Value == "," {
		p.Next()
		actions = append(actions, p.parseAction())
	}
	p.expectValue(";")
	return Trigger{Cond: conds, Actions: actions}
}

func (p *parser) parseAction() Statement {
	// move <state>
	if p.Value == "move" {
		p.Next()
		dst := p.expect("identifier")
		return &MoveStmt{Dest: dst}
	}
	// CALL(args)
	if p.Token == "identifier" {
		return p.parseCall()
	}
	p.errUnexpected("\"move\"", "\"set\"", "identifier")
	return nil
}

func (p *parser) parseCall() *Call {
	name := p.expect("identifier")
	args := make(map[string]Value)
	if p.Value == "(" {
		p.Next()
		for p.Value != ")" {
			key, value := p.parseArg()
			args[key] = value
			if p.Value != "," {
				break
			}
			p.Next() // skip comma
		}
		p.expectValue(")")
	}
	return &Call{Name: name, Args: args}
}

func (p *parser) parseParam() Arg {
	key := p.expect("identifier")
	if p.Value == "=" {
		p.Next()
		return Arg{Key: key, Value: p.parseValue()}
	}
	return Arg{Key: key}
}

func (p *parser) parseArg() (string, Value) {
	key := p.expect("identifier")
	if p.Value == "=" {
		p.Next()
		return key, p.parseValue()
	}
	return key, &ReferenceValue{Ref: key}
}

func (p *parser) parseValue() Value {
	switch p.Token {
	case "string":
		raw := p.Value
		p.Next()
		s := strings.NewReplacer(
			"\\\"", "\"",
			"\\'", "'",
			"\\a", "\a",
			"\\b", "\b",
			"\\e", "\033",
			"\\f", "\f",
			"\\n", "\n",
			"\\r", "\r",
			"\\t", "\t",
			"\\v", "\v",
			"\\\\", "\\",
		).Replace(raw[1 : len(raw)-1])
		return &ConstValue{s}
	case "int":
		s := p.Value
		p.Next()
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			panic(err)
		}
		return &ConstValue{i}
	case "float":
		s := p.Value
		p.Next()
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			panic(err)
		}
		return &ConstValue{f}
	case "bool":
		s := p.Value
		p.Next()
		return &ConstValue{s == "true"}
	case "identifier":
		s := p.Value
		p.Next()
		return &ReferenceValue{Ref: s}
	default:
		p.errUnexpected("string", "int", "float", "bool", "identifier")
		return nil
	}
}
