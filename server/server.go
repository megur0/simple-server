package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
)

/*

[パスパラメータについて]
ハンドラーには例として下記のような２つのパスを同時に登録可能な仕様としている。
・server.Get("/user/:id", ...)
・server.Get("/user/profile", ...)
しかし、このようなパスの登録は非推奨。
これは実際にリクエストパスとして"/user/profile"を実行しても、後者にはヒットせず、
前者に id = profileとしてマッチする。

ハンドラーの登録時に上記を弾くことも考えたが、
実装が面倒(つまり、/user/* かつGETを探して存在する場合にエラーとする)

*/

type Handler func(http.ResponseWriter, *http.Request)

type Middleware func(h http.Handler) http.Handler

// リクエストパスをパースして取得したパスパラメータを格納するためのテーブル
// キーはパラメータ名、バリューがパラメータの値
type pathParamTable map[string]string

type route struct {
	handler Handler
	// 今のところ、path parameterは1つしか使えない。
	pathParamName string
	middleware    []Middleware
}

// 設定情報
// これらはサーバー起動前に設定されている想定
//
// 本パッケージはnet/httpパッケージのラッパーパッケージであり、
// 内部で実行されるhttp.Server.ListenAndServeはリクエストが来るたびに、
// スレッド(Goroutine)が立ち上がる。
// このパッケージから立ち上げるListenAndServeは１つであり、
// 複数のスレッドでListenAndServeを立ち上げることは想定していない。
// そのため、各変数もスレッドセーフとはなっていない。
var (
	// ルーティング情報を格納する
	router = map[string]route{}

	commonMiddleware = []Middleware{}

	commonAfterMiddleware = []Middleware{}

	// 対象のルートが無いときに返すレスポンス
	noMethodResponse []byte = []byte(`{"message":"no method"}`)

	// 対象のルートが無いときに返すレスポンスのContentType
	noMethodContentType string = ContentTypeJSON

	// 500エラーの際に返すレスポンス
	internalServerErrorResponse []byte = []byte(`{"message":"internal server error"}`)

	// 500エラーの際に返すレスポンスのContentType
	internalServerErrorContentType string = ContentTypeJSON

	// Graceful shutdown時にタイムアウトとして設定する秒数
	ShutdownTimeoutSecond = 8 * time.Second
)

const (
	ContentTypeJSON            = "application/json"
	ContentTypeXML             = "application/xml"
	ContentTypePlainText       = "text/plain"
	ContentTypeHTML            = "text/html"
	ContentTypeFormURLEnc      = "application/x-www-form-urlencoded"
	ContentTypeMultipart       = "multipart/form-data"
	ContentTypeHTMLWithCharset = "text/html; charset=utf-8"
)

// GETメソッドのハンドラの設定
// 既に存在するパスかつメソッドを設定するとpanicになる。
// ミドルウェアは先頭から順に実行されていく。
func Get(path string, hr Handler, middleware ...Middleware) {
	setHandler(path, hr, http.MethodGet, middleware...)
}

// POSTメソッドのハンドラの設定
// 既に存在するパスかつメソッドを設定するとpanicになる。
// ミドルウェアは先頭から順に実行されていく。
func Post(path string, hr Handler, middleware ...Middleware) {
	setHandler(path, hr, http.MethodPost, middleware...)
}

// 共通のミドルウェア
// すべてのハンドラの前に実行されるミドルウェアで、先頭から順に実行されていく
// このミドルウェアはルーティング処理の前に動作する。
// 共通のミドルウェア -> 個々のミドルウェア -> 共通の後続ミドルウェア -> ルーティング処理 -> ハンドラ処理
func SetCommonMiddleware(m ...Middleware) {
	commonMiddleware = m
}

// 共通の後続ミドルウェア
// 個別のミドルウェアの後に実行されるミドルウェアを登録する
// 共通のミドルウェア -> 個々のミドルウェア -> 共通の後続ミドルウェア -> ルーティング処理 -> ハンドラ処理
// 先頭から順に実行されていく
// このミドルウェアはルーティング処理の後に動作するため、ルートが確定する前に処理が終了した場合は実行されない。
// 例えば、no mothodの場合は実行されない。
func SetCommonAfterMiddleware(m ...Middleware) {
	commonAfterMiddleware = m
}

// ルートが見つからない場合のレスポンスを設定する
// デフォルトはapplication/jsonで空のjson
func SetNoMethodResponse(contentType string, data []byte) {
	noMethodContentType = contentType
	noMethodResponse = data
}

// 500エラーの場合のレスポンスを設定する
// デフォルトはapplication/jsonで空のjson
func SetInternalServerErrorResponse(contentType string, data []byte) {
	internalServerErrorContentType = contentType
	internalServerErrorResponse = data
}

// "application/json"としてレスポンスを返す
// dataはjson.Marshalで変換を行ってレスポンスへセットする。
// json.Marshalで変換に失敗した場合はpanicとなる。
func SetResponseAsJson(w http.ResponseWriter, r *http.Request, statusCode int, data any) {
	jsn, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}

	SetResponse(w, r, ContentTypeJSON, statusCode, jsn)
}

func SetResponse(w http.ResponseWriter, r *http.Request, contentType string, statusCode int, data []byte) {
	// headerのSetは、WriteHeader関数の前に呼ぶ必要がある。
	// 後に呼んでも変更が発生しない。
	// https://pkg.go.dev/net/http#ResponseWriter
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(statusCode)
	w.Write(data)
}

// サーバーを起動する
// この関数を実行する前に、各ハンドラの設定を行う必要がある。
// シャットダウンはGraceful shutdownとなる。
//
// デフォルトのマルチプレクサ（DefaultServeMux）を利用していないため、
// 本パッケージのAPIとは別でhttp.HandleFunc("/some", /*...*/)などでルートを設定しても、
// 本関数で起動されたサーバーのルーティングには設定されないため注意。
func StartServer(c context.Context, host string, port int) {
	// マルチプレクサ（ルーティング情報）を作成してハンドラーを紐付けている。
	// ※ 作成せずにグローバルなマルチプレクサを使っても別に良かったかもしれない。
	//
	// 各パスごとにHandle関数でハンドラを設定するのではなく、
	// ルート（"/"）に対して、ルートとなるハンドラ（finalHandler）を設定している。
	// finalHandlerは、リクエストパスを開発者が登録したルーティング情報（ハンドラ／ミドルウェア）から検索して実行する。
	//
	// パスごとにHandle関数でハンドラを設定する方法（※）を採用していないのは、
	// 上記の方がコードを簡潔に書けそうだったため。
	// また、無効なパスも一旦はすべてハンドリングする構成にしたかったため。
	// （ ※ mux.Handle("/aaa") mux.Handle("/bbb") ... といった具合。）
	mux := http.NewServeMux()
	mux.Handle("/", http.HandlerFunc(recoverHandler))

	// サーバー構造体を作成
	srv := &http.Server{Addr: fmt.Sprintf("%s:%d", host, port), Handler: mux}

	// ここでgo routineを使うのはmainのスレッドではgraceful shutdownの待機をしておくため。
	go func() {
		// ListenAndServeでは、リクエストが来るたびにスレッドが起動される。
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic(fmt.Sprintf("somethig error happend on server start: %s", err))
		}
	}()

	// シャットダウンの信号待機
	shutdown = make(chan any, 1)
	defer close(shutdown) // ここでcloseしないと本ファイルのShutdown関数が待ち続けてしまう。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, os.Interrupt)
	select {
	case sig := <-quit:
		// osからのシグナルで終了
		l.Info(c, fmt.Sprintf("quit received: %v", sig))
	case sig := <-shutdown: // Shutdown関数からチャネル送信してシャットダウン
		l.Info(c, fmt.Sprintf("shutdown received: %v", sig))
	}

	// シャットダウン処理。タイムアウトを過ぎるとシャットダウン処理がキャンセルされる。
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeoutSecond)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		// net/httpのShutdown関数はgraceful shutdownを行う。
		// Error from closing listeners, or context timeout:
		panic(fmt.Sprintf("Failed to gracefully shutdown:%s", err))
	}
	l.Info(c, "Server successfully shutdowned")
}

// context.Contextにセットする値の衝突を避けるために独自のキーを使う。
type contextKey struct{ Key string }

func getContextVal(r *http.Request, key string) any {
	return r.Context().Value(contextKey{Key: key})
}

func getPathParamVal(r *http.Request, pathParamName string) string {
	pathParam := getContextVal(r, "pathParam")
	if pathParam == nil {
		// パスパラメータを含むルートへのリクエストがあった時点で、
		// finalHandlerにてpathParamの初期化がされるので、
		// getPathParamValが呼ばれた時点で、
		// pathParamが初期化されていないケースは想定外。
		panic("pathParam ")
	}
	return pathParam.(pathParamTable)[pathParamName]
}

func recoverHandler(w http.ResponseWriter, r *http.Request) {
	// panicはスタックトレースを出力してすべてinternal serverエラーとして返す。
	defer func() {
		if rv := recover(); rv != nil {
			stack := make([]uintptr, 32)
			// runtime.Callers(0), 本箇所(1), panic関数(2) をスキップしてエラー箇所を起点とする。
			n := runtime.Callers(3, stack)
			stack = stack[:n]
			frames := runtime.CallersFrames(stack)
			var trace string
			for {
				frame, more := frames.Next()
				trace += fmt.Sprintf("%s\n\t%s:%d\n", frame.Function, frame.File, frame.Line)
				if !more {
					break
				}
			}
			l.Error(r.Context(), fmt.Sprintf("panic(server recovered): %v\n", rv)+trace)
			SetResponse(w, r, internalServerErrorContentType, http.StatusInternalServerError, internalServerErrorResponse)
			return
		}
	}()

	constructHandlerBeforeRouting(0).ServeHTTP(w, r)
}

func routingHandler(w http.ResponseWriter, r *http.Request) {
	// path paramを含むpathに対応するルートを探す
	if splitPath := strings.Split(r.URL.Path, "/"); len(splitPath) > 2 {
		pathParamCandidate := string(splitPath[len(splitPath)-1])
		ru := getRoute(strings.TrimSuffix(r.URL.Path, pathParamCandidate)+":", r.Method)
		if ru != nil {
			if ru.pathParamName == "" {
				panic("path parameter name is empty")
			}
			// いま時点だとパスパラメータはひとつしか設定していない。
			// 今後複数必要になったら改良できるように、pathParamTableはマップになっている。
			pathParam := pathParamTable{}
			pathParam[ru.pathParamName] = pathParamCandidate
			ctx := context.WithValue(r.Context(), contextKey{Key: "pathParam"}, pathParam)
			r = r.WithContext(ctx)

			constructHandlerAfterRouting(0, ru).ServeHTTP(w, r)
			return
		}
	}

	// pathに対応するルートを探す
	ru := getRoute(r.URL.Path, r.Method)
	if ru != nil {
		constructHandlerAfterRouting(0, ru).ServeHTTP(w, r)
		return
	}

	// pathに対応するルートが無ければno method
	SetResponse(w, r, noMethodContentType, http.StatusNotFound, noMethodResponse)
}

// 各commonMiddleware -> routingHandlerの順に実行されるハンドラを構築する。
func constructHandlerBeforeRouting(middleWareIdx int) http.Handler {
	if middleWareIdx <= len(commonMiddleware)-1 {
		return commonMiddleware[middleWareIdx](constructHandlerBeforeRouting(middleWareIdx + 1))
	}
	return http.HandlerFunc(routingHandler)
}

// 各ルートのミドルウェア（ru.middleware） -> commonAfterMiddleware -> ルートのハンドラ処理(ru.handler)
// という順番で実行されるハンドラを構築する。
func constructHandlerAfterRouting(idx int, ru *route) http.Handler {
	middlewareIdx := idx
	if middlewareIdx <= len(ru.middleware)-1 {
		return ru.middleware[middlewareIdx](constructHandlerAfterRouting(idx+1, ru))
	}

	commonAfterMiddlewareIdx := idx - len(ru.middleware)
	if commonAfterMiddlewareIdx <= len(commonAfterMiddleware)-1 {
		return commonAfterMiddleware[commonAfterMiddlewareIdx](constructHandlerAfterRouting(idx+1, ru))
	}

	return http.HandlerFunc(ru.handler)
}

// テスト用
// shutdownチャネルはShutdown関数の方で利用するために入れている。
var shutdown chan any

// テスト用。
// shutdownチャネルに送信され、
// StartForTest関数の方に書いているチャネル受信処理（select）で受け取り、
// 後続のシャットダウン処理が走る。
func Shutdown() {
	shutdown <- struct{}{}
	<-shutdown // chがcloseするのを待つ（シャットダウン完了を待つ）
}

func getRoute(path string, method string) *route {
	r, ok := router[method+" "+path]
	if !ok {
		return nil
	}
	return &r
}

func setHandler(path string, hr Handler, method string, middleware ...Middleware) {
	paths := strings.Split(path, ":")
	pathParamName := ""
	if len(paths) > 1 {
		path = paths[0] + ":"
		pathParamName = paths[len(paths)-1]
	}

	if getRoute(path, method) != nil {
		panic(fmt.Sprintf(PanicSameRoot, path))
	}

	router[method+" "+path] = route{
		handler:       hr,
		middleware:    middleware,
		pathParamName: pathParamName,
	}
}

func wrapByErrBind(err error) *ErrBind {
	return &ErrBind{
		Err: err,
	}
}

// リクエストデータを構造体へBindする。
// 構造体以外が指定された場合はpanicとなる。
// 構造体のタグには、"json", "query", "param"を指定可能。
// タグがないフィールドが存在する場合はpanicとなる。
//
// 本関数は値のバインドのみを行い、必須フィールドのチェックは含まれない。
// したがって値が空の場合は何もしない。（何もしないので構造体はデフォルト値のままになる）
func Bind[S any](r *http.Request, s *S) error {
	rv := reflect.ValueOf(s).Elem()
	rt := rv.Type()
	if rt.Kind() != reflect.Struct {
		panic("bind arg must be pointer to struct")
	}

	body := IoReaderToString(r.Body)
	// 後続で再度読み取りできるように再度書き込む
	r.Body = io.NopCloser(bytes.NewBuffer([]byte(body)))

	// リクエストボディ -> 構造体へのbind
	if body != "" {
		// Unmarshalによる変換の際は、
		// ・json側に余分なフィールドがあってもエラーにならない。
		// ・json側に存在しない各フィールドはデフォルト値が設定される。
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

	// パラメータ、クエリー -> 構造体へのbind
	for i := range rt.NumField() {
		j := rt.Field(i).Tag.Get("json")
		if j == "" { // jsonの場合は既にbind済みのためここでは何もしない。
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
					panic("binded struct should have at least one tag, which is json or param or query")
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
	}

	return nil
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
