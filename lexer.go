package mova

import (
	"bufio"
	"errors"
	"io"
	"regexp"
	"unicode/utf8"
)

type Rule struct {
	Name    string
	Pattern *regexp.Regexp
}

type Lexer struct {
	reader *bufio.Reader
	rules  []Rule

	text     []byte
	linesize int

	Token  string
	Linenr int
	Offset int
	Length int
	Value  string
	Err    error
}

func NewLexer(reader io.Reader, rules []Rule) *Lexer {
	var lex Lexer
	lex.reader = bufio.NewReader(reader)
	lex.rules = rules
	lex.Linenr = 1

	lex.Next() /* pull first token */
	return &lex
}

func (tz *Lexer) Next() {
	// move forward
	tz.move(tz.Length)

tokenLoop:
	for {
		if len(tz.text) == 0 {
			if err := tz.readLine(); err != nil {
				if errors.Is(err, io.EOF) {
					tz.makeToken("EOF", 0)
					return
				}
				tz.Err = err
				tz.makeToken("ERROR", 0)
				return
			}
		}
		for _, r := range tz.rules {
			if loc := r.Pattern.FindIndex(tz.text); loc != nil && loc[0] == 0 {
				if r.Name == "" {
					tz.move(loc[1])
					continue tokenLoop
				}
				tz.makeToken(r.Name, loc[1])
				return
			}
		}
		_, sz := utf8.DecodeRune(tz.text)
		tz.makeToken("ILLEGAL", sz)
		return
	}
}

func (tz *Lexer) move(n int) {
	tz.text = tz.text[n:]
	tz.Offset += n
}

func (tz *Lexer) makeToken(typ string, n int) {
	tz.Token = typ
	tz.Length = n
	tz.Value = string(tz.text[:n])
}

func (tz *Lexer) readLine() error {
	tz.Linenr += tz.linesize
	tz.Offset = 0
	tz.linesize = 0

	var buf []byte
	for {
		tz.linesize++
		line, err := tz.reader.ReadBytes('\n')
		if err != nil {
			return err
		}
		if line[len(line)-1] != '\\' {
			tz.text = append(buf, line...)
			return nil
		}
		buf = append(buf, line[:len(line)-1]...)
	}
}
