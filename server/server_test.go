package server

import (
	"bytes"
	"context"
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

	SetNoMethodResponse(createResponse(false, errorDataResponse{Message: "no method"}))

	SetCommonMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			if r.Header.Get("Content-Type") != "application/json" {
				SetResponse(w, r, http.StatusBadRequest, &struct {
					Result bool `json:"is_success"`
					Data   struct {
						Message string `json:"message"`
					} `json:"data"`
				}{
					Result: false,
					Data: struct {
						Message string `json:"message"`
					}{
						Message: "content type is not application/jsonr",
					},
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	})

	SetCommonAfterMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			value := getContextVal(r, "data-setted-by-middleware")

			// after middlewareがあとから呼ばれたことを検査できるように、上書きしておく
			if value != nil {
				ctx := context.WithValue(r.Context(), contextKey{Key: "data-setted-by-middleware"}, value.(string)+" overrided by after middleware")
				r = r.WithContext(ctx)
			}

			next.ServeHTTP(w, r)
		})
	})

	SetInternalServerErrorJsonResponse(createResponse(false, errorDataResponse{Message: "something error"}))

	m.Run()
}

// go test -v -count=1 -timeout 60s -run ^TestServer$ ./server
func TestServer(t *testing.T) {
	go StartServer(context.Background(), "0.0.0.0", 8087)
	time.Sleep(time.Millisecond * 100)
	defer Shutdown()
}

// go test -v -count=1 -timeout 60s -run ^TestInternalServerError$ ./server
func TestInternalServerError(t *testing.T) {
	Get("/panic", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, nil, (*emptyDataResponse)(nil), http.StatusOK, func(req *addCommentRequest) (any, error) {
			panic("")
		})
	})
	execRequest(t, "fail", http.MethodGet, "/panic", nil, nil, http.StatusInternalServerError, &internalServerErrorJsonResponse)
}

// go test -v -count=1 -timeout 60s -run ^TestMiddlewareError$
/*
func TestMiddlewareError(t *testing.T) {
	Get("/middlewareError", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, nil, (*emptyDataResponse)(nil), http.StatusOK, func(req *any) (any, error) {
			return nil, errors.New("dummy error")
		})
	}, func(w http.ResponseWriter, r *http.Request) {
		SetResponse(ctx, sctx, http.StatusInternalServerError, &internalServerErrorJsonResponse)
		return errors.New("middleware error")
	})
	execRequest(t, "fail", http.MethodGet, "/middlewareError", nil, nil, http.StatusInternalServerError, &internalServerErrorJsonResponse)
}*/

// go test -v -count=1 -timeout 60s -run ^TestSameRoute$ ./server
func TestSameRoute(t *testing.T) {
	var r interface{}
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

// go test -v -count=1 -timeout 60s -run ^TestRootHandler$ ./server
func TestRootHandler(t *testing.T) {
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

	Get("/using-after-middleware", func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, nil, &getUserResponse{}, http.StatusOK, func(req *any) (*user, error) {
			value := getContextVal(r, "data-setted-by-middleware").(string)
			return &user{
				ID:   "",
				Name: value,
			}, nil
		})
	}, func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), contextKey{Key: "data-setted-by-middleware"}, "name")
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

	execRequest(t, "success_get_request", http.MethodGet, "/self", nil, nil, http.StatusOK, &response{
		IsSuccess: true,
		Data:      getUserResponse{},
	})

	execRequest(t, "check_after_middleware", http.MethodGet, "/using-after-middleware", nil, nil, http.StatusOK, &response{
		IsSuccess: true,
		Data: getUserResponse{
			User: user{
				Name: "name overrided by after middleware",
			},
		},
	})

	execRequest(t, "failed_not_exist_method", http.MethodPost, "/self", nil, nil, http.StatusNotFound, &response{
		IsSuccess: false,
		Data:      errorDataResponse{Message: "no method"},
	})

	execRequest(t, "failed_not_exist_path", http.MethodGet, "/user/test/test", nil, nil, http.StatusNotFound, &response{
		IsSuccess: false,
		Data:      errorDataResponse{Message: "no method"},
	})

	execRequest(t, "failed_invalid_path", http.MethodGet, "/////", nil, nil, http.StatusNotFound, &response{
		IsSuccess: false,
		Data:      errorDataResponse{Message: "no method"},
	})

	execRequest(t, "failed_invalid_path", http.MethodGet, "/\";SELECT * FROM users\"", nil, nil, http.StatusNotFound, &response{
		IsSuccess: false,
		Data:      errorDataResponse{Message: "no method"},
	})

	execRequest(t, "success_path_param", http.MethodGet, "/friend/1111", nil, nil, http.StatusOK, &response{
		IsSuccess: true,
		Data: getFriendResponse{
			Friend: friend{
				ID:   "1111",
				Name: "dummy name",
			},
		},
	})

	// 値がセットされていない場合は、リクエスト構造体には何も格納されずデフォルト値になる。
	// エラーにはならない。
	execRequest(t, "success_path_param", http.MethodGet, "/friend/", nil, nil, http.StatusOK, &response{
		IsSuccess: true,
		Data: getFriendResponse{
			Friend: friend{
				ID:   "0",
				Name: "dummy name",
			},
		},
	})

	execRequest(t, "success_post_method", http.MethodPost, "/comment", stringToIoReader(`{"comment":"test comment\ntest comment"}`), nil, http.StatusCreated, &response{
		IsSuccess: true,
		Data:      nil,
	})

	execRequest(t, "success_post_method", http.MethodPost, "/comment", stringToIoReader(`{"comment":"\";DELETE * FROM users\""}`), nil, http.StatusCreated, &response{
		IsSuccess: true,
		Data:      nil,
	})

	execRequest(t, "failed_because_invalid_json_format", http.MethodPost, "/comment", stringToIoReader(`{"comment":4`), nil, http.StatusBadRequest, &response{
		IsSuccess: false,
		Data: errorDataResponse{
			Message: newRequestBodyJsonSyntaxError(`{"comment":4`, errors.New("unexpected end of JSON input")).Error(),
		},
	})

	execRequest(t, "failed_because_invalid_field_format", http.MethodPost, "/comment", stringToIoReader(`{"comment":4}`), nil, http.StatusBadRequest, &response{
		IsSuccess: false,
		Data: errorDataResponse{
			// 本当はNewRequestBodyUnmarshalTypeErrorだが、
			// "Go struct field addCommentRequest.comment of type string"に該当する
			// reflect.Typeが分からなかった。(パッケージ内部で指定した文字列？)
			Message: newErrRequestFieldFormat("comment", errors.New("json: cannot unmarshal number into Go struct field addCommentRequest.comment of type string")).Error(),
		},
	})

	execRequest(t, "failed_because_invalid_field_format", http.MethodPost, "/uuid", stringToIoReader(`{"id":"000000000000"}`), nil, http.StatusBadRequest, &response{
		IsSuccess: false,
		Data: errorDataResponse{
			Message: newErrRequestJsonSomethingInvalid(`{"id":"000000000000"}`, errors.New("invalid UUID length: 12")).Error(),
		},
	})

	execRequest(t, "failed_path_param_because_invalid_format", http.MethodGet, "/friend/ああああ", nil, nil, http.StatusBadRequest, &response{
		IsSuccess: false,
		Data: errorDataResponse{
			Message: newErrRequestFieldFormat("number", errors.New("invalid character 'ã' looking for beginning of value")).Error(),
		},
	})

	// この場合は no methodになる。
	execRequest(t, "failed_path_param_because_required_error", http.MethodGet, "/friend", nil, nil, http.StatusNotFound, &response{
		IsSuccess: false,
		Data: errorDataResponse{
			Message: "no method",
		},
	})

	execRequest(t, "success_query_param", http.MethodGet, "/friends", nil, map[string]string{"limit": "50"}, http.StatusOK, &response{
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

	execRequest(t, "success_query_param", http.MethodGet, "/friends", nil, map[string]string{"limit": "50", "option_int_ptr": "5", "option_time_ptr": `"2006-01-02T15:04:05.000000Z"`, "option_str_ptr": `"request str"`, "option_time": `"2016-01-02T15:04:05.000000Z"`}, http.StatusOK, &response{
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

	execRequest(t, "failed_query_param_because_invalid_format", http.MethodGet, "/friends", nil, map[string]string{"limit": "あああ"}, http.StatusBadRequest, &response{
		IsSuccess: false,
		Data: errorDataResponse{
			Message: newErrRequestFieldFormat("limit", errors.New("invalid character 'ã' looking for beginning of value")).Error(),
		},
	})
}

// go test -v -count=1 -timeout 60s -run ^TestLog$ ./server
func TestLog(t *testing.T) {
	l.Debug("test %s", "test")
	l.Info("test %s", "test")
	l.Warn("test %s", "test")
	l.Error("test %s", "test")
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
			str:   `"test"`, // 「"」を付ける必要がある点に注意したし。
			check: func(t *testing.T, rv reflect.Value) { testutil.AssertEqual(t, rv.Interface().(string), "test") },
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
		t.Run("success", func(t *testing.T) {
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
			v:   reflect.ValueOf(ptr("")).Elem(),
			str: `5`,
		},
		{
			v:   reflect.ValueOf(ptr(ptr(""))).Elem(),
			str: `5`,
		},
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
		t.Run("failed", func(t *testing.T) {
			if err := setStrToStructField(v.v, v.str); err == nil {
				t.Error("should be error")
			}
		})
	}
}

func execRequest[S any](t *testing.T, testName string, method string, path string, body io.Reader, query map[string]string, statusCode int, expect *S, ignoreField ...string) {
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

	constructHandlerWithMiddleware(0).ServeHTTP(res, req)

	expectJson := toJsonString(expect)
	t.Run(testName, func(t *testing.T) {
		testutil.AssertJsonExact(t, res.Body.String(), expectJson, ignoreField)
		testutil.AssertEqual(t, res.Result().StatusCode, statusCode)
	})
}

func handle[REQ, DATA any, RES responseHasSet[DATA]](w http.ResponseWriter, r *http.Request, request *REQ, res RES, successStatusCode int, logic func(*REQ) (DATA, error)) {
	// 注意: ストリームを一度リードすると、その後はr.Bodyから読み取りをしても取得できない。
	var body string
	if r.Body != nil {
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		body = buf.String()
	}

	// Bind・バリデーション
	if request != nil {
		//logger.DV(request)
		if err := Bind(r, body, request); err != nil {
			SetResponse(w, r, http.StatusBadRequest, createResponse(false, errorDataResponse{
				Message: err.Error(),
			}))
			return
		}
	}

	// ロジック実行
	data, err := logic(request)

	if err != nil {
		SetResponse(w, r, http.StatusInternalServerError, createResponse(false, nil))
		return
	}

	// 結果を設定して返す
	res.Set(data)
	SetResponse(w, r, successStatusCode, createResponse(true, res))
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
