package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/megur0/testutil"
)

// go test -v -count=1 -timeout 60s -run ^TestSetStrToStructField$ ./server
func TestSetStrToStructField(t *testing.T) {
	for _, v := range []struct {
		explain string
		v       reflect.Value
		str     string
		check   func(*testing.T, reflect.Value)
	}{
		{
			explain: "intにセット",
			v:       reflect.ValueOf(new(int)).Elem(),
			str:     "5",
			check:   func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, rv.Interface().(int), 5) },
		},
		{
			explain: "uintにセット",
			v:       reflect.ValueOf(new(uint)).Elem(),
			str:     "5",
			check:   func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, rv.Interface().(uint), uint(5)) },
		},
		{
			explain: "byteにセット",
			v:       reflect.ValueOf(new(byte)).Elem(),
			str:     "5",
			check:   func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, rv.Interface().(byte), byte(5)) },
		},
		{
			explain: "runeにセット",
			v:       reflect.ValueOf(new(rune)).Elem(),
			str:     "5",
			check:   func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, rv.Interface().(rune), rune(5)) },
		},
		{
			explain: "stringにセット",
			v:       reflect.ValueOf(new(string)).Elem(),
			str:     "test",
			check:   func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, rv.Interface().(string), "test") },
		},
		{
			explain: "stringにセット",
			v:       reflect.ValueOf(new(string)).Elem(),
			str:     "5",
			check:   func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, rv.Interface().(string), "5") },
		},
		{
			explain: "stringに空文字をセット",
			v:       reflect.ValueOf(ptr("")).Elem(),
			str:     "",
			check:   func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, rv.Interface().(string), "") },
		},
		{
			explain: "string(ポインタ)にをセット",
			v:       reflect.ValueOf(ptr(new(string))).Elem(),
			str:     "5",
			check:   func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, *rv.Interface().(*string), "5") },
		},
		{
			explain: "boolにセット",
			v:       reflect.ValueOf(new(bool)).Elem(),
			str:     "true",
			check:   func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, rv.Interface().(bool), true) },
		},
		{
			explain: "uint8にセット",
			v:       reflect.ValueOf(new(uint8)).Elem(),
			str:     "5",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(uint8), uint8(5))
			},
		},
		{
			explain: "uint16にセット",
			v:       reflect.ValueOf(new(uint16)).Elem(),
			str:     "65535",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(uint16), uint16(65535))
			},
		},
		{
			explain: "uint32にセット",
			v:       reflect.ValueOf(new(uint32)).Elem(),
			str:     "4294967295",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(uint32), uint32(4294967295))
			},
		},
		{
			explain: "uint64にセット",
			v:       reflect.ValueOf(new(uint64)).Elem(),
			str:     "18446744073709551615",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(uint64), uint64(18446744073709551615))
			},
		},
		{
			explain: "int8にセット",
			v:       reflect.ValueOf(new(int8)).Elem(),
			str:     "127",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(int8), int8(127))
			},
		},
		{
			explain: "int16にセット",
			v:       reflect.ValueOf(new(int16)).Elem(),
			str:     "32767",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(int16), int16(32767))
			},
		},
		{
			explain: "int32にセット",
			v:       reflect.ValueOf(new(int32)).Elem(),
			str:     "2147483647",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(int32), int32(2147483647))
			},
		},
		{
			explain: "int64にセット",
			v:       reflect.ValueOf(new(int64)).Elem(),
			str:     "9223372036854775807",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(int64), int64(9223372036854775807))
			},
		},
		{
			explain: "float32にセット",
			v:       reflect.ValueOf(new(float32)).Elem(),
			str:     "1.0005",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(float32), float32(1.0005))
			},
		},
		{
			explain: "float64にセット",
			v:       reflect.ValueOf(new(float64)).Elem(),
			str:     "1.0005",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(float64), float64(1.0005))
			},
		},
		{
			explain: "UUIDにセット",
			v:       reflect.ValueOf(&uuid.UUID{}).Elem(),
			str:     uuid.UUID{}.String(),
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(uuid.UUID).String(), uuid.UUID{}.String())
			},
		},
		{
			explain: "UUID(ダブルクオテーション有)にセット",
			v:       reflect.ValueOf(&uuid.UUID{}).Elem(),
			str:     `"00000000-0000-0000-0000-000000000000"`, //UUIDのParseの処理の実装上、前後に""などの記号がついていてもエンコードできる
			// github.com/google/uuid@vX.X.X/uuid.goを参照
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(uuid.UUID).String(), "00000000-0000-0000-0000-000000000000")
			},
		},
		{
			explain: "UUID(カッコ有)にセット",
			v:       reflect.ValueOf(&uuid.UUID{}).Elem(),
			str:     `{00000000-0000-0000-0000-000000000000}`,
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(uuid.UUID).String(), "00000000-0000-0000-0000-000000000000")
			},
		},
		{
			explain: "UUID(プレフィクス有)にセット",
			v:       reflect.ValueOf(&uuid.UUID{}).Elem(),
			str:     `urn:uuid:00000000-0000-0000-0000-000000000000`,
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(uuid.UUID).String(), "00000000-0000-0000-0000-000000000000")
			},
		},
		{
			explain: "UUID(ポインタ)にセット",
			v:       reflect.ValueOf(&struct{ V *uuid.UUID }{}).Elem().Field(0),
			str:     `00000000-0000-0000-0000-000000000000`,
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(*uuid.UUID).String(), "00000000-0000-0000-0000-000000000000")
			},
		},
		{
			explain: "time.Timeにセット",
			v:       reflect.ValueOf(&time.Time{}).Elem(),
			str:     "2006-01-02T15:04:05.000000Z",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(time.Time).String(), "2006-01-02 15:04:05 +0000 UTC")
			},
		},
		{
			explain: "time.Timeにセット",
			v:       reflect.ValueOf(&struct{ V time.Time }{}).Elem().Field(0),
			str:     "2006-01-02T15:04:05.000000Z",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(time.Time).String(), "2006-01-02 15:04:05 +0000 UTC")
			},
		},
		{
			explain: "time.Time（ポインタ）にセット",
			v:       reflect.ValueOf(&struct{ V *time.Time }{}).Elem().Field(0),
			str:     "2006-01-02T15:04:05.000000Z",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(*time.Time).String(), "2006-01-02 15:04:05 +0000 UTC")
			},
		},
		{
			explain: "json.Unmarshalerを実装した型にセット",
			v:       reflect.ValueOf(&struct{ V testUnmarshaler }{}).Elem().Field(0),
			str:     `"test"`,
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(testUnmarshaler).String(), "test")
			},
		},
		{
			explain: "json.Unmarshaler(ポインタ)を実装した型にセット",
			v:       reflect.ValueOf(&struct{ V *testUnmarshaler }{}).Elem().Field(0),
			str:     `"test"`,
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(*testUnmarshaler).String(), "test")
			},
		},
		{
			explain: "json.TextUnmarshalerを実装した型にセット",
			v:       reflect.ValueOf(&struct{ V testTextUnmarshaler }{}).Elem().Field(0),
			str:     "test",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(testTextUnmarshaler).String(), "test")
			},
		},
		{
			explain: "json.TextUnmarshaler(ポインタ)を実装した型にセット",
			v:       reflect.ValueOf(&struct{ V *testTextUnmarshaler }{}).Elem().Field(0),
			str:     "test",
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(*testTextUnmarshaler).String(), "test")
			},
		},
	} {
		t.Run("成功: "+v.explain, func(t *testing.T) {
			if err := setStrToStructField(v.v, v.str); err != nil {
				t.Fatal("unexpected result:", err)
			}
			v.check(t, v.v)
		})
	}

	for _, v := range []struct {
		explain string
		v       reflect.Value
		str     string
	}{
		{
			explain: "intに文字列をセット",
			v:       reflect.ValueOf(ptr(0)).Elem(),
			str:     "test",
		},
		{
			explain: "intのポインタに文字列をセット",
			v:       reflect.ValueOf(ptr(ptr(0))).Elem(),
			str:     "test",
		},
		{
			explain: "UUIDに文字列をセット",
			v:       reflect.ValueOf(ptr(uuid.UUID{})).Elem(),
			str:     "test",
		},
		{
			explain: "UUIDのポインタに文字列をセット",
			v:       reflect.ValueOf(ptr(ptr(uuid.UUID{}))).Elem(),
			str:     "test",
		},
		{
			explain: "time.Timeの形式誤り",
			v:       reflect.ValueOf(&time.Time{}).Elem(),
			str:     "2006",
		},
		{
			explain: "time.Timeの形式誤り",
			v:       reflect.ValueOf(&struct{ V time.Time }{}).Elem().Field(0),
			str:     "2006",
		},
	} {
		t.Run("失敗: "+v.explain, func(t *testing.T) {
			if err := setStrToStructField(v.v, v.str); err == nil {
				t.Error("should be error")
			}
		})
	}
}

type MyTypeWithUnmarshal struct {
	uuid string
}

func (s *MyTypeWithUnmarshal) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	if str == "" {
		return errors.New("empty string")
	}
	_, err := uuid.Parse(str)
	if err != nil {
		return errors.New("invalid uuid")
	}
	s.uuid = str
	return nil
}

type MyTypeWithUnmarshalText struct {
	uuid string
}

func (s *MyTypeWithUnmarshalText) UnmarshalText(b []byte) error {
	str := string(b)
	if str == "" {
		return errors.New("empty string")
	}
	_, err := uuid.Parse(str)
	if err != nil {
		return errors.New("invalid uuid")
	}
	s.uuid = str
	return nil
}

// go test -v -count=1 -timeout 60s -run ^TestBind$ ./server
func TestBind(t *testing.T) {
	resetSetting()

	t.Run("成功: JSONリクエスト", func(t *testing.T) {
		type testStruct struct {
			Field1 string                  `json:"field1"`
			Field2 int                     `json:"field2"`
			Field3 *string                 `json:"field3"`
			Field4 *int                    `json:"field4"`
			Field5 time.Time               `json:"field5"`
			Field6 uuid.UUID               `json:"field6"`
			Field7 MyTypeWithUnmarshal     `json:"field7"`
			Field8 *MyTypeWithUnmarshal    `json:"field8"`
			Field9 MyTypeWithUnmarshalText `json:"field9"`
		}

		body := `{
			"field1": "test string",
			"field2": 123,
			"field3": "optional string",
			"field4": 456,
			"field5": "2006-01-02T15:04:05.000000Z",
			"field6": "00000000-0000-0000-0000-000000000000",
			"field7": "0976b7cd-988b-45a7-a48a-af527c1ed9e3",
			"field8": "0976b7cd-988b-45a7-a48a-af527c1ed9e3",
			"field9": "0976b7cd-988b-45a7-a48a-af527c1ed9e3"
		}`

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		var result testStruct
		err := Bind(req, &result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		testutil.AssertEqual(t, result.Field1, "test string")
		testutil.AssertEqual(t, result.Field2, 123)
		testutil.AssertEqual(t, *result.Field3, "optional string")
		testutil.AssertEqual(t, *result.Field4, 456)
		testutil.AssertEqual(t, result.Field5.String(), "2006-01-02 15:04:05 +0000 UTC")
		testutil.AssertEqual(t, result.Field6.String(), "00000000-0000-0000-0000-000000000000")
		testutil.AssertEqual(t, result.Field7.uuid, "0976b7cd-988b-45a7-a48a-af527c1ed9e3")
		testutil.AssertEqual(t, result.Field8.uuid, "0976b7cd-988b-45a7-a48a-af527c1ed9e3")
		testutil.AssertEqual(t, result.Field9.uuid, "0976b7cd-988b-45a7-a48a-af527c1ed9e3")
	})

	t.Run("成功: クエリーパラメータ", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?"+getFormData(map[string]string{
			"field1": "test",
			"field2": "123",
		}), nil)

		var result struct {
			Field1 string  `query:"field1"`
			Field2 int     `query:"field2"`
			Field3 *string `query:"field3"`
		}
		err := Bind(req, &result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		testutil.AssertEqual(t, result.Field1, "test")
		testutil.AssertEqual(t, result.Field2, 123)
		testutil.AssertEqual(t, result.Field3, (*string)(nil))
	})

	t.Run("成功: パスパラメータ(int)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test/123", nil)
		ctx := context.WithValue(req.Context(), contextKey{Key: "pathParam"}, pathParamTable{"id": "123"})
		req = req.WithContext(ctx)

		var result struct {
			ID int `param:"id"`
		}
		err := Bind(req, &result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		testutil.AssertEqual(t, result.ID, 123)
	})

	t.Run("成功: パスパラメータ(UUID)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test/0976b7cd-988b-45a7-a48a-af527c1ed9e3", nil)
		ctx := context.WithValue(req.Context(), contextKey{Key: "pathParam"}, pathParamTable{"id": "0976b7cd-988b-45a7-a48a-af527c1ed9e3"})
		req = req.WithContext(ctx)

		var result struct {
			ID uuid.UUID `param:"id"`
		}
		err := Bind(req, &result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		testutil.AssertEqual(t, result.ID.String(), "0976b7cd-988b-45a7-a48a-af527c1ed9e3")
	})

	t.Run("成功: パスパラメータ(独自型でUnmarshalTextを実装)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test/0976b7cd-988b-45a7-a48a-af527c1ed9e3", nil)
		ctx := context.WithValue(req.Context(), contextKey{Key: "pathParam"}, pathParamTable{"id": "0976b7cd-988b-45a7-a48a-af527c1ed9e3"})
		req = req.WithContext(ctx)

		var result struct {
			ID MyTypeWithUnmarshalText `param:"id"`
		}
		err := Bind(req, &result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		testutil.AssertEqual(t, result.ID.uuid, "0976b7cd-988b-45a7-a48a-af527c1ed9e3")
	})

	t.Run("成功: パスパラメータ(独自型でUnmarshalを実装)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test/\"0976b7cd-988b-45a7-a48a-af527c1ed9e3\"", nil)
		ctx := context.WithValue(req.Context(), contextKey{Key: "pathParam"}, pathParamTable{"id": "\"0976b7cd-988b-45a7-a48a-af527c1ed9e3\""})
		req = req.WithContext(ctx)

		var result struct {
			ID MyTypeWithUnmarshal `param:"id"`
		}
		err := Bind(req, &result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		testutil.AssertEqual(t, result.ID.uuid, "0976b7cd-988b-45a7-a48a-af527c1ed9e3")
	})

	t.Run("成功: フォームリクエスト", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(getFormData(map[string]string{
			"field1": "test",
			"field2": "123",
			"field4": "",
			"field5": "test\n\rtest",
		})))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		var result struct {
			Field1 string  `form:"field1"`
			Field2 int     `form:"field2"`
			Field3 *string `form:"field3"`
			Field4 *string `form:"field4"`
			Field5 string  `form:"field5"`
		}
		err := Bind(req, &result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		testutil.AssertEqual(t, result.Field1, "test")
		testutil.AssertEqual(t, result.Field2, 123)
		// フィールド自体が無い場合は値のセットが行われない
		testutil.AssertEqual(t, result.Field3, (*string)(nil))
		// 値がない場合（空文字の場合）でもセットされる
		testutil.AssertEqual(t, *result.Field4, "")
		testutil.AssertEqual(t, result.Field5, "test\n\rtest")
	})

	t.Run("失敗: 不正なJSON", func(t *testing.T) {
		body := `{"field1": "test", "field2": "invalid"}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		var result struct {
			Field1 string `json:"field1"`
			Field2 int    `json:"field2"`
		}
		err := Bind(req, &result)
		if err == nil {
			t.Fatal("expected error but got nil")
		}

		var bindErr *ErrBind
		if !errors.As(err, &bindErr) {
			t.Fatalf("unexpected error type: %v", err)
		}
	})

	t.Run("失敗: 不正なクエリーパラメータ", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?field2=invalid", nil)

		var result struct {
			Field2 int `query:"field2"`
		}
		err := Bind(req, &result)
		if err == nil {
			t.Fatal("expected error but got nil")
		}

		var bindErr *ErrBind
		if !errors.As(err, &bindErr) {
			t.Fatalf("unexpected error type: %v", err)
		}
	})

	t.Run("失敗: 不正なパスパラメータ", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test/invalid", nil)
		ctx := context.WithValue(req.Context(), contextKey{Key: "pathParam"}, pathParamTable{"id": "invalid"})
		req = req.WithContext(ctx)

		var result struct {
			ID int `param:"id"`
		}
		err := Bind(req, &result)
		if err == nil {
			t.Fatal("expected error but got nil")
		}

		var bindErr *ErrBind
		if !errors.As(err, &bindErr) {
			t.Fatalf("unexpected error type: %v", err)
		}
	})
}

func getFormData(m map[string]string) string {
	formData := url.Values{}
	for key, value := range m {
		formData.Set(key, value)
	}
	return formData.Encode()
}
