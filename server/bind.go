package server

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

// リクエストデータを構造体へBindする。
// 構造体以外が指定された場合はpanicとなる。
// 構造体のタグには、"json", "query", "param", "form"を指定可能。
// タグがないフィールドが存在する場合はpanicとなる。
// "multipart/form-data"はサポートしていない。
//
// 本関数は値のバインドのみを行い、必須フィールドのチェックは含まれない。
// 対象のフィールドが含まれない場合は何もセットしない。
// その場合は構造体はデフォルト値のままになる。
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
		var fieldValue *string
		p := rt.Field(i).Tag.Get("param")
		if p != "" {
			fieldName = p
			val := getPathParamVal(r, p)

			// 空の場合はセットを行わない。
			// 例えば/friend/:idといったパスに対してマッチするのは
			// /friend/1234といった形式のみであり、/friendはマッチしないため、
			// ここでもし空文字が取得される場合はそもそもパス指定の中にパスパラメータが
			// 含まれていないケースとなる。
			if val != "" {
				fieldValue = &val
			}
		} else {
			q := rt.Field(i).Tag.Get("query")
			if q != "" {
				fieldName = q
				val, ok := r.URL.Query()[fieldName]
				if ok {
					fieldValue = &val[0]
				}
			} else {
				f := rt.Field(i).Tag.Get("form")
				if f != "" {
					if !isFormRequest {
						panic("form tag is only available in form request")
					}
					fieldName = f
					val, ok := r.Form[fieldName]
					if ok {
						fieldValue = &val[0]
					}
				} else {
					panic("binded struct should have at least one tag, which is json or param or query")
				}
			}
		}
		if fieldValue == nil {
			// リクエストに含まれていない場合はsetStrToStructFieldは実行しない。
			// この場合は構造体はゼロバリューのままとなる。
			continue
		}
		if err := setStrToStructField(rv.Field(i), *fieldValue); err != nil {
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

// structの各要素へURLのクエリやパスパラメータから取得したstringをセットする。
// 特徴として、encoding.TextUnmarshalerやjson.Unmarshalerを実装している型に対しては
// UnmarshalTextやUnmarshalJSONを実行する。
//
// 空文字の場合(例えば?hoge=&fuge=)でもセット処理が実行される。
func setStrToStructField(rv reflect.Value, str string) error {
	if !rv.CanSet() {
		panic("should only use canset value")
	}

	// encoding.TextUnmarshalerやjson.Unmarshalerは基本的に
	// ポインタレシーバーであるため、値型の場合はマッチしない。
	// したがって型の判定はポインタに対して行う。
	rva := rv
	if rv.Kind() != reflect.Ptr {
		rva = rv.Addr()
	}
	switch rva.Interface().(type) {
	case *string:
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.ValueOf(&str))
		} else {
			rv.Set(reflect.ValueOf(str))
		}
	case *uint:
		var v uint64
		v, err := strconv.ParseUint(str, 10, strconv.IntSize)
		if err != nil {
			return err
		}
		vv := uint(v)
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.ValueOf(&vv))
		} else {
			rv.Set(reflect.ValueOf(vv))
		}
	case *int:
		var v int
		v, err := strconv.Atoi(str)
		if err != nil {
			return err
		}
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.ValueOf(&v))
		} else {
			rv.Set(reflect.ValueOf(v))
		}
	case *bool:
		var v bool
		v, err := strconv.ParseBool(str)
		if err != nil {
			return err
		}
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.ValueOf(&v))
		} else {
			rv.Set(reflect.ValueOf(v))
		}
	case *int8:
		var v int64
		v, err := strconv.ParseInt(str, 10, 8)
		if err != nil {
			return err
		}
		vv := int8(v)
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.ValueOf(&vv))
		} else {
			rv.Set(reflect.ValueOf(vv))
		}
	case *int16:
		var v int64
		v, err := strconv.ParseInt(str, 10, 16)
		if err != nil {
			return err
		}
		vv := int16(v)
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.ValueOf(&vv))
		} else {
			rv.Set(reflect.ValueOf(vv))
		}
	case *int32:
		var v int64
		v, err := strconv.ParseInt(str, 10, 32)
		if err != nil {
			return err
		}
		vv := int32(v)
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.ValueOf(&vv))
		} else {
			rv.Set(reflect.ValueOf(vv))
		}
	case *int64:
		var v int64
		v, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			return err
		}
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.ValueOf(&v))
		} else {
			rv.Set(reflect.ValueOf(v))
		}
	case *uint8:
		var v uint64
		v, err := strconv.ParseUint(str, 10, 8)
		if err != nil {
			return err
		}
		vv := uint8(v)
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.ValueOf(&vv))
		} else {
			rv.Set(reflect.ValueOf(vv))
		}
	case *uint16:
		var v uint64
		v, err := strconv.ParseUint(str, 10, 16)
		if err != nil {
			return err
		}
		vv := uint16(v)
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.ValueOf(&vv))
		} else {
			rv.Set(reflect.ValueOf(vv))
		}
	case *uint32:
		var v uint64
		v, err := strconv.ParseUint(str, 10, 32)
		if err != nil {
			return err
		}
		vv := uint32(v)
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.ValueOf(&vv))
		} else {
			rv.Set(reflect.ValueOf(vv))
		}
	case *uint64:
		var v uint64
		v, err := strconv.ParseUint(str, 10, 64)
		if err != nil {
			return err
		}
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.ValueOf(&v))
		} else {
			rv.Set(reflect.ValueOf(v))
		}
	case *float32:
		var v float64
		v, err := strconv.ParseFloat(str, 32)
		if err != nil {
			return err
		}
		vv := float32(v)
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.ValueOf(&vv))
		} else {
			rv.Set(reflect.ValueOf(vv))
		}
	case *float64:
		var v float64
		v, err := strconv.ParseFloat(str, 64)
		if err != nil {
			return err
		}
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.ValueOf(&v))
		} else {
			rv.Set(reflect.ValueOf(v))
		}
	case encoding.TextUnmarshaler:
		// 例えば*uuid.UUIDや*time.Timeがヒットする。
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.New(rv.Type().Elem()))
			if err := rv.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(str)); err != nil {
				return err
			}
		} else {
			if err := rva.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(str)); err != nil {
				return err
			}
		}
	case json.Unmarshaler:
		// UnmarshalJSONを実装している型がヒットする
		if rv.Kind() == reflect.Ptr {
			rv.Set(reflect.New(rv.Type().Elem()))
			if err := rv.Interface().(json.Unmarshaler).UnmarshalJSON([]byte(str)); err != nil {
				return err
			}
		} else {
			if err := rva.Interface().(json.Unmarshaler).UnmarshalJSON([]byte(str)); err != nil {
				return err
			}
		}
	default:
		panic(fmt.Sprintf("unsupported type: %T", rv.Interface()))
	}

	return nil
}

func wrapByErrBind(err error) *ErrBind {
	return &ErrBind{
		Err: err,
	}
}
