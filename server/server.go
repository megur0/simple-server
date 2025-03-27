package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
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
func StartServer(c context.Context, host string, port int) {
	// 各パスごとにHandle関数でハンドラを設定するのではなく、
	// ルート（"/"）に対して、ルートとなるハンドラを設定している。
	// recoverHandlerは後続処理でpanicが発生した場合のリカバリーとスタックトレース、
	// 後続のroutingHandlerは、リクエストパスを開発者が登録したルーティング情報（ハンドラ／ミドルウェア）から検索して実行する。
	//
	// パスごとにHandle関数でハンドラを設定する方法（※）を採用していないのは、
	// 上記の方がコードを簡潔に書けそうだったため。
	// また、無効なパスも一旦はすべてハンドリングする構成にしたかったため。
	// （ ※ http.Handle("/aaa") http.Handle("/bbb") ... といった具合。）
	http.Handle("/", http.HandlerFunc(recoverHandler))
	srv := &http.Server{Addr: fmt.Sprintf("%s:%d", host, port)}

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