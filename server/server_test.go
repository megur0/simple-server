package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/megur0/testutil"
)

func InitTimezone() {
	var err error
	time.Local, err = time.LoadLocation("Asia/Tokyo")
	if err != nil {
		panic(fmt.Sprintf("timezone init failed:%s", err))
	}
}

// go test -v -count=1 ./server
func TestMain(m *testing.M) {
	// タイムゾーン初期化
	InitTimezone()

	m.Run()
}

func resetSetting() {
	SetNoMethodResponse("application/json", GetErrorResponseJson("no method"))
	SetInternalServerErrorResponse("application/json", GetErrorResponseJson("something error"))
	SetCommonAfterMiddleware()
	SetCommonMiddleware()
	router = map[string]route{}
}

// go test -v -count=1 -timeout 60s -run ^TestServer$ ./server
func TestServer(t *testing.T) {
	resetSetting()
	go StartServer(context.Background(), "0.0.0.0", 8087)
	time.Sleep(time.Millisecond * 100)
	defer Shutdown()
}

// go test -v -count=1 -timeout 60s -run ^TestInternalServerError$ ./server
func TestInternalServerError(t *testing.T) {
	t.Run("レスポンスに空のバイトを設定", func(t *testing.T) {
		resetSetting()
		SetInternalServerErrorResponse("application/json", []byte{})
		Get("/panic", func(w http.ResponseWriter, r *http.Request) {
			handle(w, r, nil, (*emptyDataResponse)(nil), http.StatusOK, func(req *addCommentRequest) (any, error) {
				panic("dummy panic")
			})
		})
		execRequest[any](t, http.MethodGet, "/panic", nil, nil, http.StatusInternalServerError, nil)
	})

	t.Run("レスポンスにnilを設定", func(t *testing.T) {
		resetSetting()
		SetInternalServerErrorResponse("application/json", nil)
		Get("/panic", func(w http.ResponseWriter, r *http.Request) {
			handle(w, r, nil, (*emptyDataResponse)(nil), http.StatusOK, func(req *addCommentRequest) (any, error) {
				panic("dummy panic")
			})
		})
		execRequest[any](t, http.MethodGet, "/panic", nil, nil, http.StatusInternalServerError, nil)
	})

	t.Run("ハンドラー内でpanic", func(t *testing.T) {
		resetSetting()
		Get("/panic", func(w http.ResponseWriter, r *http.Request) {
			handle(w, r, nil, (*emptyDataResponse)(nil), http.StatusOK, func(req *addCommentRequest) (any, error) {
				panic("dummy panic")
			})
		})
		execRequest(t, http.MethodGet, "/panic", nil, nil, http.StatusInternalServerError, createResponse(false, errorDataResponse{Message: "something error"}))
	})

	t.Run("ミドルウェア内でpanic", func(t *testing.T) {
		resetSetting()
		middleware := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("dummy panic")
			})
		}

		Get("/panic", func(w http.ResponseWriter, r *http.Request) {
			SetResponseAsJson(w, r, http.StatusOK, "dummy")
		}, middleware)
		execRequest(t, http.MethodGet, "/panic", nil, nil, http.StatusInternalServerError, createResponse(false, errorDataResponse{Message: "something error"}))
	})

	t.Run("共通ミドルウェア内でpanic", func(t *testing.T) {
		resetSetting()
		commonMiddleware1 := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("dummy panic")
			})
		}
		SetCommonMiddleware(commonMiddleware1)

		Get("/panic", func(w http.ResponseWriter, r *http.Request) {
			SetResponseAsJson(w, r, http.StatusOK, "dummy")
		})
		execRequest(t, http.MethodGet, "/panic", nil, nil, http.StatusInternalServerError, createResponse(false, errorDataResponse{Message: "something error"}))
	})

	t.Run("共通の後続ミドルウェア内でpanic", func(t *testing.T) {
		resetSetting()
		commonAfterMiddleware1 := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("dummy panic")
			})
		}
		SetCommonAfterMiddleware(commonAfterMiddleware1)

		Get("/panic", func(w http.ResponseWriter, r *http.Request) {
			SetResponseAsJson(w, r, http.StatusOK, "dummy")
		})
		execRequest(t, http.MethodGet, "/panic", nil, nil, http.StatusInternalServerError, createResponse(false, errorDataResponse{Message: "something error"}))
	})
}

// Bindした後もBodyが読み込めることを確認
// go test -v -count=1 -timeout 60s -run ^TestRequestBodyReadableAfterBind$ ./server
func TestRequestBodyReadableAfterBind(t *testing.T) {
	resetSetting()

	type testRequest struct {
		Field string `json:"field"`
	}

	Get("/read-body", func(w http.ResponseWriter, r *http.Request) {
		var req testRequest
		if err := Bind(r, &req); err != nil {
			SetResponseAsJson(w, r, http.StatusBadRequest, createResponse(false, errorDataResponse{
				Message: err.Error(),
			}))
			return
		}

		body := IoReaderToString(r.Body)
		SetResponseAsJson(w, r, http.StatusOK, createResponse(true, body))
	})

	requestBody := `{"field":"test value"}`

	t.Run("Bind実行後にリクエストボディが読み込めることを確認", func(t *testing.T) {
		body := stringToIoReader(requestBody)
		execRequest(t, http.MethodGet, "/read-body", body, nil, http.StatusOK, &response{
			IsSuccess: true,
			Data:      requestBody,
		})
	})
}

// go test -v -count=1 -timeout 60s -run ^TestSameRoute$ ./server
func TestSameRoute(t *testing.T) {
	resetSetting()
	var r any
	defer func() {
		if r = recover(); r == nil {
			t.Fatalf("should get panic")
		}
		testutil.AssertEqual(t, r, fmt.Sprintf(PanicSameRoot, "/test"))
	}()
	Get("/test", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, nil, (*emptyDataResponse)(nil), http.StatusOK, func(req *addCommentRequest) (any, error) {
			return nil, nil
		})
	})
	Get("/test", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, nil, (*emptyDataResponse)(nil), http.StatusOK, func(req *addCommentRequest) (any, error) {
			return nil, nil
		})
	})
}

// go test -v -count=1 -timeout 60s -run ^TestMiddlewareOrder$ ./server
func TestMiddlewareOrder(t *testing.T) {
	resetSetting()
	var middlewareExecutionOrder []string

	commonMiddleware1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareExecutionOrder = append(middlewareExecutionOrder, "commonMiddleware1")
			next.ServeHTTP(w, r)
		})
	}

	commonMiddleware2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareExecutionOrder = append(middlewareExecutionOrder, "commonMiddleware2")
			next.ServeHTTP(w, r)
		})
	}

	SetCommonMiddleware(commonMiddleware1, commonMiddleware2)

	commonAfterMiddleware1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareExecutionOrder = append(middlewareExecutionOrder, "commonAfterMiddleware1")
			next.ServeHTTP(w, r)
		})
	}

	commonAfterMiddleware2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareExecutionOrder = append(middlewareExecutionOrder, "commonAfterMiddleware2")
			next.ServeHTTP(w, r)
		})
	}

	SetCommonAfterMiddleware(commonAfterMiddleware1, commonAfterMiddleware2)

	// Middleware 1
	middleware1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareExecutionOrder = append(middlewareExecutionOrder, "middleware1")
			next.ServeHTTP(w, r)
		})
	}

	// Middleware 2
	middleware2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareExecutionOrder = append(middlewareExecutionOrder, "middleware2")
			next.ServeHTTP(w, r)
		})
	}

	// Middleware 3
	middleware3 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareExecutionOrder = append(middlewareExecutionOrder, "middleware3")
			next.ServeHTTP(w, r)
		})
	}

	Get("/middleware-order-test", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, nil, (*emptyDataResponse)(nil), http.StatusOK, func(req *any) (any, error) {
			return nil, nil
		})
	}, middleware1, middleware2, middleware3)

	t.Run("ミドルウェアの実行順序が想定通り", func(t *testing.T) {
		execRequest(t, http.MethodGet, "/middleware-order-test", nil, nil, http.StatusOK, &response{
			IsSuccess: true,
			Data:      nil,
		})

		expectedOrder := []string{"commonMiddleware1", "commonMiddleware2", "middleware1", "middleware2", "middleware3", "commonAfterMiddleware1", "commonAfterMiddleware2"}
		if !reflect.DeepEqual(middlewareExecutionOrder, expectedOrder) {
			t.Fatalf("unexpected middleware execution order: got %v, want %v", middlewareExecutionOrder, expectedOrder)
		}
	})
}

// go test -v -count=1 -timeout 60s -run ^TestHandler$ ./server
func TestHandler(t *testing.T) {
	resetSetting()
	Get("/self", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, nil, &getUserResponse{}, http.StatusOK, func(req *any) (*user, error) {
			user := getContextVal(r, "user").(*user)
			return user, nil
		})
	}, func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			//auth := c.Request.Header.Get("Authorization")
			//idToken := strings.Replace(auth, "Bearer ", "", 1)

			ctx := context.WithValue(r.Context(), contextKey{Key: "user"}, &user{})
			ctx = context.WithValue(ctx, contextKey{Key: "uid"}, "test uid")
			r = r.WithContext(ctx)
			h.ServeHTTP(w, r)
		})
	})

	Get("/friend/:number", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, &getFriendRequest{}, &getFriendResponse{}, http.StatusOK, func(req *getFriendRequest) (*friend, error) {
			return &friend{
				ID:   strconv.Itoa(req.Number),
				Name: "dummy name",
			}, nil
		})
	})

	Get("/friends", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, &getFriendsRequest{}, &getFriendsResponse{}, http.StatusOK, func(req *getFriendsRequest) ([]friend, error) {

			friends := []friend{
				{
					ID:            "1",
					Name:          "friend1",
					OptionIntPtr:  getOrZeroValFromPtr(req.OptionIntPtr),
					OptionStrPtr:  getOrZeroValFromPtr(req.OptionStrPtr),
					OptionTimePtr: getOrZeroValFromPtr(req.OptionTimePtr),
					OptionTime:    req.OptionTime,
				},
				{
					ID:            "2",
					Name:          "friend2",
					OptionIntPtr:  getOrZeroValFromPtr(req.OptionIntPtr),
					OptionStrPtr:  getOrZeroValFromPtr(req.OptionStrPtr),
					OptionTimePtr: getOrZeroValFromPtr(req.OptionTimePtr),
					OptionTime:    req.OptionTime,
				},
			}
			return friends, nil
		})
	})

	Post("/comment", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, &addCommentRequest{}, (*emptyDataResponse)(nil), http.StatusCreated, func(req *addCommentRequest) (any, error) {
			return nil, nil
		})
	})

	Post("/uuid", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, &uuidRequest{}, (*emptyDataResponse)(nil), http.StatusCreated, func(req *uuidRequest) (any, error) {
			return nil, nil
		})
	})

	t.Run("成功：GET /self", func(t *testing.T) {
		execRequest(t, http.MethodGet, "/self", nil, nil, http.StatusOK, &response{
			IsSuccess: true,
			Data:      getUserResponse{},
		})
	})

	t.Run("失敗：存在しないメソッド", func(t *testing.T) {
		execRequest(t, http.MethodPost, "/self", nil, nil, http.StatusNotFound, &response{
			IsSuccess: false,
			Data:      errorDataResponse{Message: "no method"},
		})
	})

	t.Run("失敗：存在しないパス", func(t *testing.T) {
		execRequest(t, http.MethodGet, "/user/test/test", nil, nil, http.StatusNotFound, &response{
			IsSuccess: false,
			Data:      errorDataResponse{Message: "no method"},
		})
	})

	t.Run("失敗：不正なパス", func(t *testing.T) {
		execRequest(t, http.MethodGet, "/////", nil, nil, http.StatusNotFound, &response{
			IsSuccess: false,
			Data:      errorDataResponse{Message: "no method"},
		})
	})

	t.Run("失敗：不正なパス (SQL Injection)", func(t *testing.T) {
		execRequest(t, http.MethodGet, "/\";SELECT * FROM users\"", nil, nil, http.StatusNotFound, &response{
			IsSuccess: false,
			Data:      errorDataResponse{Message: "no method"},
		})
	})

	t.Run("成功：パスパラメータ", func(t *testing.T) {
		execRequest(t, http.MethodGet, "/friend/1111", nil, nil, http.StatusOK, &response{
			IsSuccess: true,
			Data: getFriendResponse{
				Friend: friend{
					ID:   "1111",
					Name: "dummy name",
				},
			},
		})
	})

	t.Run("成功：パスパラメータ (デフォルト値)", func(t *testing.T) {
		// 値がセットされていない場合は、リクエスト構造体には何も格納されずデフォルト値になる。
		// エラーにはならない。
		execRequest(t, http.MethodGet, "/friend/", nil, nil, http.StatusOK, &response{
			IsSuccess: true,
			Data: getFriendResponse{
				Friend: friend{
					ID:   "0",
					Name: "dummy name",
				},
			},
		})
	})

	t.Run("成功：POST /comment", func(t *testing.T) {
		execRequest(t, http.MethodPost, "/comment", stringToIoReader(`{"comment":"test comment\ntest comment"}`), nil, http.StatusCreated, &response{
			IsSuccess: true,
			Data:      nil,
		})
	})

	t.Run("成功：POST /comment (SQL Injection)", func(t *testing.T) {
		execRequest(t, http.MethodPost, "/comment", stringToIoReader(`{"comment":"\";DELETE * FROM users\""}`), nil, http.StatusCreated, &response{
			IsSuccess: true,
			Data:      nil,
		})
	})

	t.Run("失敗：不正なJSONフォーマット", func(t *testing.T) {
		execRequest(t, http.MethodPost, "/comment", stringToIoReader(`{"comment":4`), nil, http.StatusBadRequest, &response{
			IsSuccess: false,
			Data: errorDataResponse{
				Message: newRequestBodyJsonSyntaxError(`{"comment":4`, errors.New("unexpected end of JSON input")).Error(),
			},
		})
	})

	t.Run("失敗：不正なフォーマット", func(t *testing.T) {
		execRequest(t, http.MethodPost, "/comment", stringToIoReader(`{"comment":4}`), nil, http.StatusBadRequest, &response{
			IsSuccess: false,
			Data: errorDataResponse{
				// 本当はNewRequestBodyUnmarshalTypeErrorだが、
				// "Go struct field addCommentRequest.comment of type string"に該当する
				// reflect.Typeが分からなかった。(パッケージ内部で指定した文字列？)
				Message: newErrRequestFieldFormat("comment", errors.New("json: cannot unmarshal number into Go struct field addCommentRequest.comment of type string")).Error(),
			},
		})
	})

	t.Run("失敗：不正なフォーマット", func(t *testing.T) {
		execRequest(t, http.MethodPost, "/uuid", stringToIoReader(`{"id":"000000000000"}`), nil, http.StatusBadRequest, &response{
			IsSuccess: false,
			Data: errorDataResponse{
				Message: newErrRequestJsonSomethingInvalid(`{"id":"000000000000"}`, errors.New("invalid UUID length: 12")).Error(),
			},
		})
	})

	t.Run("パスパラメータ失敗: 不正なフォーマット", func(t *testing.T) {
		execRequest(t, http.MethodGet, "/friend/ああああ", nil, nil, http.StatusBadRequest, &response{
			IsSuccess: false,
			Data: errorDataResponse{
				Message: newErrRequestFieldFormat("number", errors.New("invalid character 'ã' looking for beginning of value")).Error(),
			},
		})
	})

	t.Run("パスパラメータの失敗: 必須項目", func(t *testing.T) {
		// この場合は no methodになる。
		execRequest(t, http.MethodGet, "/friend", nil, nil, http.StatusNotFound, &response{
			IsSuccess: false,
			Data: errorDataResponse{
				Message: "no method",
			},
		})
	})

	t.Run("クエリーパラメータ成功1", func(t *testing.T) {
		execRequest(t, http.MethodGet, "/friends", nil, map[string]string{"limit": "50"}, http.StatusOK, &response{
			IsSuccess: true,
			Data: getFriendsResponse{
				List: []friend{
					{
						ID:   "1",
						Name: "friend1",
					},
					{
						ID:   "2",
						Name: "friend2",
					},
				},
			},
		})
	})

	t.Run("クエリーパラメータ成功2", func(t *testing.T) {
		execRequest(t, http.MethodGet, "/friends", nil, map[string]string{"limit": "50", "option_int_ptr": "5", "option_time_ptr": `"2006-01-02T15:04:05.000000Z"`, "option_str_ptr": `"request str"`, "option_time": `"2016-01-02T15:04:05.000000Z"`}, http.StatusOK, &response{
			IsSuccess: true,
			Data: getFriendsResponse{
				List: []friend{
					{
						ID:            "1",
						Name:          "friend1",
						OptionIntPtr:  5,
						OptionStrPtr:  `"request str"`,
						OptionTimePtr: testutil.GetFirst(time.Parse("2006-01-02T15:04:05.000000Z07:00", "2006-01-02T15:04:05.000000Z")),
						OptionTime:    testutil.GetFirst(time.Parse("2006-01-02T15:04:05.000000Z07:00", "2016-01-02T15:04:05.000000Z")),
					},
					{
						ID:            "2",
						Name:          "friend2",
						OptionIntPtr:  5,
						OptionStrPtr:  `"request str"`,
						OptionTimePtr: testutil.GetFirst(time.Parse("2006-01-02T15:04:05.000000Z07:00", "2006-01-02T15:04:05.000000Z")),
						OptionTime:    testutil.GetFirst(time.Parse("2006-01-02T15:04:05.000000Z07:00", "2016-01-02T15:04:05.000000Z")),
					},
				},
			},
		})

	})

	t.Run("クエリーパラメータ失敗:不正なパラメータ", func(t *testing.T) {
		execRequest(t, http.MethodGet, "/friends", nil, map[string]string{"limit": "あああ"}, http.StatusBadRequest, &response{
			IsSuccess: false,
			Data: errorDataResponse{
				Message: newErrRequestFieldFormat("limit", errors.New("invalid character 'ã' looking for beginning of value")).Error(),
			},
		})
	})
}

// go test -v -count=1 -timeout 60s -run ^TestLog$ ./server
func TestLog(t *testing.T) {
	l.Debug(context.Background(), "test", "test2")
	l.Info(context.Background(), "test", "test2")
	l.Warn(context.Background(), "test", "test2")
	l.Error(context.Background(), "test", "test2")
	SetLogger(l)
}

func stringToIoReader(str string) *strings.Reader {
	return strings.NewReader(str)
}

// go test -v -count=1 -timeout 60s -run ^TestSetStrToStructField$ ./server
func TestSetStrToStructField(t *testing.T) {
	for _, v := range []struct {
		v     reflect.Value
		str   string
		check func(*testing.T, reflect.Value)
	}{
		{
			v:     reflect.ValueOf(ptr(0)).Elem(),
			str:   "",
			check: func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, rv.Interface().(int), 0) },
		},
		{
			v:     reflect.ValueOf(ptr(0)).Elem(),
			str:   "5",
			check: func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, rv.Interface().(int), 5) },
		},
		{
			v:     reflect.ValueOf(ptr("")).Elem(),
			str:   `"test"`,
			check: func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, rv.Interface().(string), "test") },
		},
		{
			v:     reflect.ValueOf(ptr("")).Elem(),
			str:   `5`,
			check: func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, rv.Interface().(string), "5") },
		},
		{
			v:     reflect.ValueOf(ptr(ptr(""))).Elem(),
			str:   `5`,
			check: func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, *rv.Interface().(*string), "5") },
		},
		{
			v:   reflect.ValueOf(&time.Time{}).Elem(),
			str: `"2006-01-02T15:04:05.000000Z"`,
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(time.Time).String(), "2006-01-02 15:04:05 +0000 UTC")
			},
		},
		{
			v:   reflect.ValueOf(&uuid.UUID{}).Elem(),
			str: `"00000000-0000-0000-0000-000000000000"`,
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(uuid.UUID).String(), "00000000-0000-0000-0000-000000000000")
			},
		},
		{
			v:   reflect.ValueOf(&struct{ V time.Time }{}).Elem().Field(0),
			str: `"2006-01-02T15:04:05.000000Z"`,
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(time.Time).String(), "2006-01-02 15:04:05 +0000 UTC")
			},
		},
		{
			v:   reflect.ValueOf(&struct{ V *uuid.UUID }{}).Elem().Field(0),
			str: `"00000000-0000-0000-0000-000000000000"`,
			check: func(t *testing.T, rv reflect.Value) {
				testutil.AssertEqual(t, rv.Interface().(*uuid.UUID).String(), "00000000-0000-0000-0000-000000000000")
			},
		},
	} {
		t.Run("success for "+v.str, func(t *testing.T) {
			if err := setStrToStructField(v.v, v.str); err != nil {
				t.Fatal("unexpected result:", err)
			}
			v.check(t, v.v)
		})
	}

	for _, v := range []struct {
		v   reflect.Value
		str string
	}{
		{
			v:   reflect.ValueOf(ptr(0)).Elem(),
			str: `"test"`,
		},
		{
			v:   reflect.ValueOf(ptr(ptr(0))).Elem(),
			str: `"test"`,
		},
		{
			v:   reflect.ValueOf(ptr(uuid.UUID{})).Elem(),
			str: `"test"`,
		},
		{
			v:   reflect.ValueOf(ptr(ptr(uuid.UUID{}))).Elem(),
			str: `"test"`,
		},
		{
			v:   reflect.ValueOf(&time.Time{}).Elem(),
			str: "2006",
		},
		{
			v:   reflect.ValueOf(&struct{ V time.Time }{}).Elem().Field(0),
			str: "2006",
		},
	} {
		t.Run("failed for "+v.str, func(t *testing.T) {
			if err := setStrToStructField(v.v, v.str); err == nil {
				t.Error("should be error")
			}
		})
	}
}

// go test -v -count=1 -timeout 60s -run ^TestHTMLResponse$ ./server
func TestHTMLResponse(t *testing.T) {
	resetSetting()
	htmlContent := "<html><body><h1>Hello, World!</h1></body></html>"

	Get("/html", func(w http.ResponseWriter, r *http.Request) {
		SetResponse(w, r, "text/html; charset=utf-8", http.StatusOK, []byte(htmlContent))
	})

	req, _ := http.NewRequest(http.MethodGet, "/html", nil)
	res := httptest.NewRecorder()
	http.HandlerFunc(recoverHandler).ServeHTTP(res, req)

	t.Run("", func(t *testing.T) {
		testutil.AssertEqual(t, res.Body.String(), htmlContent)
		testutil.AssertEqual(t, res.Result().StatusCode, http.StatusOK)
	})
}

// go test -v -count=1 -timeout 60s -run ^TestBind$ ./server
func TestBind(t *testing.T) {
	resetSetting()

	type testStruct struct {
		Field1 string    `json:"field1"`
		Field2 int       `json:"field2"`
		Field3 *string   `json:"field3"`
		Field4 *int      `json:"field4"`
		Field5 time.Time `json:"field5"`
		Field6 uuid.UUID `json:"field6"`
	}

	t.Run("成功: JSONリクエスト", func(t *testing.T) {
		body := `{
			"field1": "test string",
			"field2": 123,
			"field3": "optional string",
			"field4": 456,
			"field5": "2006-01-02T15:04:05.000000Z",
			"field6": "00000000-0000-0000-0000-000000000000"
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
	})

	t.Run("成功: クエリーパラメータ", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?field1=test&field2=123", nil)

		var result struct {
			Field1 string `query:"field1"`
			Field2 int    `query:"field2"`
		}
		err := Bind(req, &result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		testutil.AssertEqual(t, result.Field1, "test")
		testutil.AssertEqual(t, result.Field2, 123)
	})

	t.Run("成功: パスパラメータ", func(t *testing.T) {
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

	t.Run("成功: フォームリクエスト", func(t *testing.T) {
		body := "field1=test&field2=123"
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		var result struct {
			Field1 string `form:"field1"`
			Field2 int    `form:"field2"`
		}
		err := Bind(req, &result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		testutil.AssertEqual(t, result.Field1, "test")
		testutil.AssertEqual(t, result.Field2, 123)
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

func execRequest[S any](t *testing.T, method string, path string, body io.Reader, query map[string]string, statusCode int, expect *S, ignoreField ...string) {
	t.Helper()
	req, _ := http.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")

	q := req.URL.Query()
	for key, val := range query {
		q.Set(key, val)
	}
	req.URL.RawQuery = q.Encode()
	//logger.DV(req.URL)

	res := httptest.NewRecorder()

	http.HandlerFunc(recoverHandler).ServeHTTP(res, req)

	if expect != nil {
		expectJson := toJsonString(expect)
		testutil.AssertJsonExact(t, res.Body.String(), expectJson, ignoreField)
		testutil.AssertEqual(t, res.Result().StatusCode, statusCode)
	}
}

func handle[REQ, DATA any, RES responseHasSet[DATA]](w http.ResponseWriter, r *http.Request, request *REQ, res RES, successStatusCode int, logic func(*REQ) (DATA, error)) {
	// Bind・バリデーション
	if request != nil {
		//logger.DV(request)
		if err := Bind(r, request); err != nil {
			SetResponseAsJson(w, r, http.StatusBadRequest, createResponse(false, errorDataResponse{
				Message: err.Error(),
			}))
			return
		}
	}

	// ロジック実行
	data, err := logic(request)

	if err != nil {
		SetResponseAsJson(w, r, http.StatusInternalServerError, createResponse(false, nil))
		return
	}

	// 結果を設定して返す
	res.Set(data)
	SetResponseAsJson(w, r, successStatusCode, createResponse(true, res))
}

type responseHasSet[U any] interface {
	Set(U)
}

type response struct {
	IsSuccess bool `json:"is_success"`
	Data      any  `json:"data"`
}

func createResponse(isSuccess bool, data any) *response {
	return &response{
		IsSuccess: isSuccess,
		Data:      data,
	}
}

type emptyDataResponse struct{}

func (r *emptyDataResponse) Set(i any) {}

type errorDataResponse struct {
	Message string `json:"message"`
}

func GetErrorResponseJson(message string) []byte {
	jsn, err := json.Marshal(*createResponse(false, errorDataResponse{Message: message}))
	if err != nil {
		panic(err)
	}
	return jsn
}

func (r *errorDataResponse) Set(s string) {
	r.Message = s
}

type getUserResponse struct {
	User user `json:"user"`
}
type user struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (r *getUserResponse) Set(entity *user) {
	r.User = *entity
}

type getFriendRequest struct {
	Number int `param:"number"`
}
type getFriendResponse struct {
	Friend friend `json:"friend"`
}
type friend struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	OptionIntPtr  int       `json:"option_int_ptr"`
	OptionStrPtr  string    `json:"option_str_ptr"`
	OptionTimePtr time.Time `json:"option_time_ptr"`
	OptionTime    time.Time `json:"option_time"`
}

func (r *getFriendResponse) Set(entity *friend) {
	r.Friend = *entity
}

type getFriendsRequest struct {
	Limit         int        `query:"limit"`
	OptionIntPtr  *int       `query:"option_int_ptr"`
	OptionStrPtr  *string    `query:"option_str_ptr"`
	OptionTimePtr *time.Time `query:"option_time_ptr"`
	OptionTime    time.Time  `query:"option_time"`
}
type getFriendsResponse struct {
	List []friend `json:"list"`
}

func (r *getFriendsResponse) Set(list []friend) {
	r.List = list
}

type addCommentRequest struct {
	Comment string `json:"comment"`
}

type uuidRequest struct {
	Id uuid.UUID `json:"id"`
}

// 以下はテスト用のエラー生成関数
func newRequestBodyJsonSyntaxError(jsn string, err error) *ErrBind {
	return &ErrBind{
		Err: &ErrRequestJsonSyntaxError{
			Json: jsn,
			Err:  err,
		},
	}
}

// テストでNewRequestBodyUnmarshalTypeErrorのreflect.Typeで上手くエラーメッセージを再現できなかったためこちらの関数を利用
func newErrRequestFieldFormat(field string, err error) *ErrBind {
	return &ErrBind{
		Err: &ErrRequestFieldFormat{
			Field: field,
			Err:   err,
		},
	}
}

func newErrRequestJsonSomethingInvalid(json string, err error) *ErrBind {
	return &ErrBind{
		Err: &ErrRequestJsonSomethingInvalid{
			Json: json,
			Err:  err,
		},
	}
}
