package server

import (
	"encoding/json"
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
