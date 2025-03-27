# WEBサーバーやWEB APIを立ち上げるためのシンプルなパッケージ
* net/httpパッケージのラッパーパッケージ

# 特徴
* コード量が少ない軽量なパッケージ
* サーバーの起動
	* panicが発生した際のスタックトレース出力
	* Graceful shutdown
* ルーティング機能
* 3種類のミドルウェアの指定
	* ルーティング処理前に共通で実行されるミドルウェア
	* 各ルート毎に設定可能なミドルウェア
	* 各ルート毎のミドルウェア実行後に実行する共通のミドルウェア
* リクエストデータのバインド
	* パラメータとしてjson、form、パスパラメータ、クエリーパラメータに対応
    * Bind関数を呼ぶことでリクエストのデータを構造体へバインドする
    * 構造体には"json", "form", "query", "param"で指定

# サンプルコード
```go
package main

import (
    "github.com/megur0/simple-server/server"
)

func main() {
    middleware1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// some middleware logic
			next.ServeHTTP(w, r)
		})
	}

    middleware2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// some middleware logic
			next.ServeHTTP(w, r)
		})
	}

    server.Post("/hello/:field3", func(w http.ResponseWriter, r *http.Request) {
		req := struct {
            Field1 string `json:"field1"`
            Field2 string `query:"field2"`
            Field3 string `param:"field3"`
            Field4 string `form:"field4"`
        }{}
		if err := server.Bind(r, &req); err != nil {
			server.SetResponseAsJson(w, r, http.StatusBadRequest, err)
			return
		}
		server.SetResponseAsJson(w, r, http.StatusOK, "request body is "+req.Field1+", request query is "+req.Field2+", request param is "+req.Field3+", request form is "+req.Field4)
	}, middleware1, middleware2)

    server.StartServer(context.Background(), "localhost", 8080)
}
```
