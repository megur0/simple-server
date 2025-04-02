package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/google/uuid"
)

// リクエストデータを構造体へBindする。
// 構造体以外が指定された場合はpanicとなる。
// 構造体のタグには、"json", "query", "param", "form"を指定可能。
// タグがないフィールドが存在する場合はpanicとなる。
// "multipart/form-data"はサポートしていない。
//
// 本関数は値のバインドのみを行い、必須フィールドのチェックは含まれない。
// したがって値が空の場合は何もしない。（何もしないので構造体はデフォルト値のままになる）
func Bind[S any](r *http.Request, s *S) error {
	// "multipart/form-data"はサポートしていない。
	// 指定されていない場合はチェックしない。
	contentType := r.Header.Get("Content-Type")
	if contentType != "" && !isFormRequest(r) && !strings.HasPrefix(contentType, "application/json") {
		return errors.New("Content-Type is not supported:" + r.Header.Get("Content-Type"))
	}

	rv := reflect.ValueOf(s).Elem()
	rt := rv.Type()
	if rt.Kind() != reflect.Struct {
		panic("bind arg must be pointer to struct")
	}

	body := IoReaderToString(r.Body)
	// 後続で再度読み取りできるように再度書き込む
	r.Body = io.NopCloser(bytes.NewBuffer([]byte(body)))

	// リクエストボディ -> 構造体へのbind
	if body != "" && !isFormRequest(r) {
		// Unmarshalによる変換の際は、
		// ・json側に余分なフィールドがあってもエラーにならない。
		// ・json側に存在しない構造体のフィールドは何も上書きされない。
		if err := json.Unmarshal([]byte(body), s); err != nil {
			// json自体のシンタックスエラー
			jsonUnmarshalErrSyntaxErr := &json.SyntaxError{}
			if errors.As(err, &jsonUnmarshalErrSyntaxErr) {
				return wrapByErrBind(&ErrRequestJsonSyntaxError{
					Json: body,
					Err:  err,
				})
			}
			// intやboolなどの組み込み型において、型の不一致の場合のエラー
			// ※ tpやUUID.uuidのUnmarshalJSONによるエラーはここには入らない。
			jsonUnmarshalErrTypeErr := &json.UnmarshalTypeError{}
			if errors.As(err, &jsonUnmarshalErrTypeErr) {
				return wrapByErrBind(&ErrRequestFieldFormat{
					Field: jsonUnmarshalErrTypeErr.Field,
					Err:   err,
				})
			}

			// 上記以外のエラーはErrRequestJsonSomethingInvalidでラップする。
			// どのフィールドのエラーなのか、という情報も返すことが理想だが、そのようにはできなかった。
			// ※ json.Unmarshalは、個別に定義した型のUnmarshalJSONを実行してエラーが発生した場合に、そのerrorをそのまま返すため。
			return wrapByErrBind(&ErrRequestJsonSomethingInvalid{
				Json: body,
				Err:  err,
			})
		}
	}

	isFormRequest := isFormRequest(r)
	if isFormRequest {
		if err := r.ParseForm(); err != nil {
			return wrapByErrBind(&ErrRequestFormParse{
				Err: err,
			})
		}
	}

	// パラメータ、クエリー、フォーム -> 構造体へのbind
	for i := range rt.NumField() {
		j := rt.Field(i).Tag.Get("json")
		if j != "" { // jsonの場合は既にbind済みのためここでは何もしない。
			continue
		}

		var fieldName string
		var fieldValue string
		p := rt.Field(i).Tag.Get("param")
		if p != "" {
			fieldName = p
			fieldValue = getPathParamVal(r, p) // 空だったとしても空文字が取得されるので問題ない。
		} else {
			q := rt.Field(i).Tag.Get("query")
			if q != "" {
				fieldName = q
				fieldValue = r.URL.Query().Get(q)
			} else {
				f := rt.Field(i).Tag.Get("form")
				if f != "" {
					if !isFormRequest {
						panic("form tag is only available in form request")
					}
					fieldName = f
					fieldValue = r.FormValue(f)
				} else {
					panic("binded struct should have at least one tag, which is json or param or query")
				}
			}
		}
		if fieldValue == "" {
			// 値がセットされていない or リクエストに含まれていない場合は
			// setStrToStructFieldは実行しない。（したがって構造体はゼロバリューのまま）
			continue
		}
		if err := setStrToStructField(rv.Field(i), fieldValue); err != nil {
			return wrapByErrBind(&ErrRequestFieldFormat{
				Field: fieldName,
				Err:   err,
			})
		}
	}

	return nil
}

func isFormRequest(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	return strings.HasPrefix(contentType, "application/x-www-form-urlencoded")
}

// structの各要素へURLのクエリやパスパラメータから取得したstringをセットする用途。
func setStrToStructField(rv reflect.Value, str string) error {
	if !rv.CanSet() {
		panic("should only use canset value")
	}
	if str == "" {
		return nil
	}

	switch rv.Interface().(type) {
	case string:
		var s string
		b := []byte(str)
		// 文字列がダブルクォートで囲まれていないとjson.Unmarshalでエラーとなるため、
		// ここでダブルクォートで囲む。
		if !strings.HasPrefix(str, `"`) || !strings.HasSuffix(str, `"`) {
			b = []byte(`"` + str + `"`)
		}
		err := json.Unmarshal(b, &s)
		if err != nil {
			return err
		}
		rv.SetString(s)
	case *string:
		var s string
		b := []byte(str)
		if !strings.HasPrefix(str, `"`) || !strings.HasSuffix(str, `"`) {
			b = []byte(`"` + str + `"`)
		}
		err := json.Unmarshal(b, &s)
		if err != nil {
			return err
		}
		rv.Set(reflect.ValueOf(&str))
	case int:
		var i int
		err := json.Unmarshal([]byte(str), &i)
		if err != nil {
			return err
		}
		// SetIntだと引数がint64で型エラーになるのでSetメソッドを使った。
		// もっと良いやり方があるかもしれない。
		rv.Set(reflect.ValueOf(i))
	case *int:
		var i int
		err := json.Unmarshal([]byte(str), &i)
		if err != nil {
			return err
		}
		rv.Set(reflect.ValueOf(&i))
	case uuid.UUID:
		var u uuid.UUID
		err := json.Unmarshal([]byte(str), &u)
		if err != nil {
			return err
		}
		rv.Set(reflect.ValueOf(u))
	case *uuid.UUID:
		var u uuid.UUID
		err := json.Unmarshal([]byte(str), &u)
		if err != nil {
			return err
		}
		rv.Set(reflect.ValueOf(&u))
	case json.Marshaler: // time.Time, *time.Timeや、Marshalerを実装した型がヒットする。
		// case json.Unmarshaler: としていないのは、
		// これはメソッドがポインタレシーバーのため、値型の場合（time.Time等）がヒットしなくなるため。
		var mr json.Unmarshaler
		var ok bool
		if rv.Kind() == reflect.Ptr {
			ev := reflect.New(rv.Type().Elem()) // ※ Newはポインターが帰る
			rv.Set(ev)
			mr, ok = ev.Interface().(json.Unmarshaler)
		} else {
			mr, ok = rv.Addr().Interface().(json.Unmarshaler)
		}
		if !ok {
			panic(fmt.Sprintf("this type is no Unmarshaler: %#v, %s", rv, rv.Type()))
		}
		if err := mr.UnmarshalJSON([]byte(str)); err != nil {
			return err
		}
	default:
		panic(fmt.Sprintf("unexpected type: %T", rv))
	}

	return nil
}

func wrapByErrBind(err error) *ErrBind {
	return &ErrBind{
		Err: err,
	}
}

