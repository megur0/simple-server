# シンプルなサーバー
* net/httpパッケージのラッパーパッケージ

# 特徴
* コード量が少ない軽量なパッケージ
* ミドルウェアの指定
* パラメータとしてボディ(JSON)、パスパラメータ、クエリーパラメータに対応
    * Bind関数を呼ぶことでリクエストのデータを構造体へバインドする
    * 構造体には"json", "query", "param"で指定
* Graceful shutdown

# サンプルコード
```go
package main

import (
    "github.com/megur0/simple-server"
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
        }{}
		if err := server.Bind(r, &req); err != nil {
			server.SetResponseAsJson(w, r, http.StatusBadRequest, err)
			return
		}
		server.SetResponseAsJson(w, r, http.StatusOK, "request body is "+req.Field1+", request query is "+req.Field2+", request param is "+req.Field3)
	}, middleware1, middleware2)

    server.StartServer(context.Background(), "localhost", 8080)
}
```

# TODO
* エラー時のレスポンス
    * 現状、no methodやinternal server errorの際にapplication/jsonで返している
    * これをカスタマイズできるようにする。