// Copyright © 2016 Alan A. A. Donovan & Brian W. Kernighan.
// License: https://creativecommons.org/licenses/by-nc-sa/4.0/
// Copyright © 2016 Yoshiki Shibata. All rights reserved.

// See page 344.

// Package sexpr provides a means for converting Go objects to and
// from S-expressions.
package sexpr

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"text/scanner"
)

// !+Unmarshal
// Unmarshal parses S-expression data and populates the variable
// whose address is in the non-nil pointer out.
func Unmarshal(data []byte, out interface{}) (err error) {
	lex := &lexer{scan: scanner.Scanner{Mode: scanner.GoTokens}}
	lex.scan.Init(bytes.NewReader(data))
	lex.next() // get the first token
	defer func() {
		// NOTE: this is not an example of ideal error handling.
		if x := recover(); x != nil {
			err = fmt.Errorf("error at %s: %v", lex.scan.Position, x)
		}
	}()

	//+ Exercise 12.12
	// Build map of tags to field names
	tags := make(map[string]string)
	v := reflect.ValueOf(out).Elem() // the struct variable
	for i := 0; i < v.NumField(); i++ {
		fieldInfo := v.Type().Field(i) // a reflect.StructField
		tag := fieldInfo.Tag           // a reflect.StructTag
		name := tag.Get("sexpr")
		if name == "" {
			name = fieldInfo.Name
		}
		tags[name] = fieldInfo.Name
	}
	//- Exercise 12.12

	read(lex, reflect.ValueOf(out).Elem(), tags)
	return nil
}

//!-Unmarshal

// !+lexer
type lexer struct {
	scan  scanner.Scanner
	token rune // the current token
}

func (lex *lexer) next()        { lex.token = lex.scan.Scan() }
func (lex *lexer) text() string { return lex.scan.TokenText() }

func (lex *lexer) consume(want rune) {
	if lex.token != want { // NOTE: Not an example of good error handling.
		panic(fmt.Sprintf("got %q, want %q", lex.text(), want))
	}
	lex.next()
}

//!-lexer

// The read function is a decoder for a small subset of well-formed
// S-expressions.  For brevity of our example, it takes many dubious
// shortcuts.
//
// The parser assumes
// - that the S-expression input is well-formed; it does no error checking.
// - that the S-expression input corresponds to the type of the variable.
// - that all numbers in the input are non-negative decimal integers.
// - that all keys in ((key value) ...) struct syntax are unquoted symbols.
// - that the input does not contain dotted lists such as (1 2 . 3).
// - that the input does not contain Lisp reader macros such 'x and #'x.
//
// The reflection logic assumes
// - that v is always a variable of the appropriate type for the
//   S-expression value.  For example, v must not be a boolean,
//   interface, channel, or function, and if v is an array, the input
//   must have the correct number of elements.
// - that v in the top-level call to read has the zero value of its
//   type and doesn't need clearing.
// - that if v is a numeric variable, it is a signed integer.

// !+read
func read(lex *lexer, v reflect.Value, tags map[string]string) {
	switch lex.token {
	case scanner.Ident:
		// The only valid identifiers are
		// "t", "nil", and struct field names.
		switch lex.text() {
		case "nil":
			v.Set(reflect.Zero(v.Type()))
			lex.next()
			return
		//+ Exercise 12.3
		case "t":
			v.SetBool(true)
			lex.next()
			return
			//- Exercise 12.3
		}

	case scanner.String:
		s, _ := strconv.Unquote(lex.text()) // NOTE: ignoring errors
		v.SetString(s)
		lex.next()
		return

	case scanner.Int:
		i, _ := strconv.Atoi(lex.text()) // NOTE: ignoring errors
		if isSignedInt(v) {              // Exercise 12.10
			v.SetInt(int64(i))
		} else {
			v.SetUint(uint64(i)) // Exercise 12.10
		}
		lex.next()
		return

	//+ Exercise 12.3
	case scanner.Float:
		switch v.Kind() {
		case reflect.Float32:
			f, _ := strconv.ParseFloat(lex.text(), 32) // NOTE: ignoring erros
			v.SetFloat(f)
		case reflect.Float64:
			f, _ := strconv.ParseFloat(lex.text(), 64) // NOTE: ignoring erros
			v.SetFloat(f)
		default:
			panic(fmt.Sprintf("unexpected type: %d", v.Kind()))
		}
		lex.next()
		return

	case '#':
		lex.next() // Ident
		lex.next() // '('
		lex.next() // Float
		r := lex.text()
		lex.next() // Float
		i := lex.text()
		lex.next() // ')'
		lex.consume(')')

		var bitSize int
		switch v.Kind() {
		case reflect.Complex64:
			bitSize = 32
		case reflect.Complex128:
			bitSize = 64
		default:
			panic(fmt.Sprintf("unexpected type: %d", v.Kind()))
		}
		fr, _ := strconv.ParseFloat(r, bitSize)
		fi, _ := strconv.ParseFloat(i, bitSize)
		v.SetComplex(complex(fr, fi))
		return
	//- Exercise 12.3

	case '(':
		lex.next()
		readList(lex, v, tags)
		lex.next() // consume ')'
		return

	}
	panic(fmt.Sprintf("unexpected token %d %q", lex.token, lex.text()))
}

//!-read

// !+readlist
func readList(lex *lexer, v reflect.Value, tags map[string]string) {
	switch v.Kind() {
	case reflect.Array: // (item ...)
		for i := 0; !endList(lex); i++ {
			read(lex, v.Index(i), tags)
		}

	case reflect.Slice: // (item ...)
		for !endList(lex) {
			item := reflect.New(v.Type().Elem()).Elem()
			read(lex, item, tags)
			v.Set(reflect.Append(v, item))
		}

	case reflect.Struct: // ((name value) ...)
		for !endList(lex) {
			lex.consume('(')
			if lex.token != scanner.Ident {
				panic(fmt.Sprintf("got token %q, want field name", lex.text()))
			}
			name := lex.text()
			lex.next()
			read(lex, v.FieldByName(tags[name]), tags) // Exercise 12.12
			lex.consume(')')
		}

	case reflect.Map: // ((key value) ...)
		v.Set(reflect.MakeMap(v.Type()))
		for !endList(lex) {
			lex.consume('(')
			key := reflect.New(v.Type().Key()).Elem()
			read(lex, key, tags)
			value := reflect.New(v.Type().Elem()).Elem()
			read(lex, value, tags)
			v.SetMapIndex(key, value)
			lex.consume(')')
		}

	//+ Exercise 12.3
	case reflect.Interface: //
		t, _ := strconv.Unquote(lex.text())
		value := reflect.New(typeOf(t)).Elem()
		lex.next()
		read(lex, value, tags)
		v.Set(value)
	//- Exercise 12.3

	default:
		panic(fmt.Sprintf("cannot decode list into %v", v.Type()))
	}
}

func endList(lex *lexer) bool {
	switch lex.token {
	case scanner.EOF:
		panic("end of file")
	case ')':
		return true
	}
	return false
}

//!-readlist

// + Exercise 12.10
var maps = map[string]reflect.Type{
	"int":        reflect.TypeOf(int(0)),
	"int8":       reflect.TypeOf(int8(0)),
	"int16":      reflect.TypeOf(int16(0)),
	"int32":      reflect.TypeOf(int32(0)),
	"int64":      reflect.TypeOf(int64(0)),
	"uint":       reflect.TypeOf(uint(0)),
	"uint8":      reflect.TypeOf(uint8(0)),
	"uint16":     reflect.TypeOf(uint16(0)),
	"uint32":     reflect.TypeOf(uint32(0)),
	"uint64":     reflect.TypeOf(uint64(0)),
	"bool":       reflect.TypeOf(false),
	"string":     reflect.TypeOf(""),
	"complex64":  reflect.TypeOf(complex64(0 + 0i)),
	"complex128": reflect.TypeOf(complex128(0 + 0i)),
}

// typeOf returns reflect.Type, but does not support all primitive types yet
// and cannot support all possible types.
func typeOf(tName string) reflect.Type {
	t, ok := maps[tName]
	if ok {
		return t
	}

	// slice
	if strings.HasPrefix(tName, "[]") {
		return reflect.SliceOf(typeOf(tName[2:]))
	}

	// array
	if tName[0] == '[' {
		i := strings.Index(tName, "]")
		if i > 0 {
			len, _ := strconv.Atoi(tName[1:i]) // NOTE: ignoring errors
			return reflect.ArrayOf(len, typeOf(tName[i+1:]))
		}
	}

	if strings.HasPrefix(tName, "map") {
		i := strings.Index(tName, "]")
		if i > 0 {
			return reflect.MapOf(typeOf(tName[4:i]), typeOf(tName[i+1:]))
		}
	}

	panic(fmt.Sprintf("%s not supported yet\n", tName))
}

func isSignedInt(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16,
		reflect.Int32, reflect.Int64:
		return true

	case reflect.Uint, reflect.Uint8, reflect.Uint16,
		reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return false

	default:
		panic(fmt.Sprintf("isSignedInt: v.Kind(%d) not supported", v.Kind()))
	}
}

//- Exercise 12.10
