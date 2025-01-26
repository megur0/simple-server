package server

import (
	"fmt"
)

/*

Errはnilにならない想定(必ずラップしている)のためError関数内で
e.Errがnilかどうかをチェックをしていない。
Errがnilの場合はErr.Error()が実行時エラーとなる。

*/

var (
	PanicSameRoot = "there already route %s exists"
)

type ErrBind struct {
	Err error
}

func (e *ErrBind) Error() string {
	return fmt.Sprintf("bind error:%s", e.Err.Error())
}

func (e *ErrBind) Unwrap() error {
	return e.Err
}

type ErrRequestJsonSomethingInvalid struct {
	Json string
	Err  error
}

func (e *ErrRequestJsonSomethingInvalid) Error() string {
	return fmt.Sprintf("something json invalid:%s, json:%s", e.Err.Error(), e.Json)
}

func (e *ErrRequestJsonSomethingInvalid) Unwrap() error {
	return e.Err
}

type ErrRequestJsonSyntaxError struct {
	Json string
	Err  error
}

func (e *ErrRequestJsonSyntaxError) Error() string {
	return fmt.Sprintf("json format invalid:%s, json:%s", e.Err.Error(), e.Json)
}

func (e *ErrRequestJsonSyntaxError) Unwrap() error {
	return e.Err
}

type ErrRequestFieldFormat struct {
	Field string
	Err   error
}

func (e *ErrRequestFieldFormat) Error() string {
	return fmt.Sprintf("json field invalid:%s, field:%s", e.Err.Error(), e.Field)
}

func (e *ErrRequestFieldFormat) Unwrap() error {
	return e.Err
}
