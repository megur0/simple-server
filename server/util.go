package server

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
)

func ptr[T any](a T) *T {
	return &a
}

func getOrZeroValFromPtr[R any](a *R) R {
	if a == nil {
		return *new(R)
	}
	return *a
}

func toJsonString(from any) string {
	if rv := reflect.ValueOf(from); (rv.Kind() == reflect.Map || rv.Kind() == reflect.Pointer) && rv.IsNil() {
		return ""
	}
	jsn, err := json.Marshal(from)
	if err != nil {
		panic(err)
	}
	return string(jsn)
}

func IoReaderToString(ir io.Reader) string {
	if ir == nil {
		return ""
	}
	buf := new(bytes.Buffer)
	buf.ReadFrom(ir)
	return buf.String()
}
