# シンプルなサーバー
* net/httpパッケージのラッパーパッケージ

# 特徴
* ミドルウェア
* パラメータとしてボディ（JSON）、パスパラメータ、クエリーパラメータに対応
* リクエストのデータを構造体へbind
    * 構造体に"json", "query", "param"で指定
* Graceful shutdown

# サンプルコード
* テストコードを参照

# TODO
* エラー時のレスポンス
    * 現状、no methodやinternal server errorの際にapplication/jsonで返している
    * これをカスタマイズできるようにする。