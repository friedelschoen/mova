package mova

import (
	"bufio"
	"errors"
	"io"
	"regexp"
	"unicode/utf8"
)

type rule struct {
	Name    string
	Pattern *regexp.Regexp
}

type lexer struct {
	reader *bufio.Reader
	rules  []rule

	text     []byte
	linesize int

	Token  string
	Linenr int
	Offset int
	Length int
	Value  string
	Err    error
}

func newLexer(reader io.Reader, rules []rule) *lexer {
	var lex lexer
	lex.reader = bufio.NewReader(reader)
	lex.rules = rules
	lex.Linenr = 1

	lex.Next() /* pull first token */
	return &lex
}

func (tz *lexer) move(n int) {
	tz.text = tz.text[n:]
	tz.Offset += n
}

func (tz *lexer) makeToken(typ string, n int) {
	tz.Token = typ
	tz.Length = n
	tz.Value = string(tz.text[:n])
}

func (tz *lexer) readLine() error {
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

func (tz *lexer) Next() {
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
