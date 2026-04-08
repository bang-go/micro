package umeng

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPrepareConfig(t *testing.T) {
	t.Run("validate required config", func(t *testing.T) {
		_, err := prepareConfig(nil)
		if !errors.Is(err, ErrNilConfig) {
			t.Fatalf("expected ErrNilConfig, got %v", err)
		}

		_, err = prepareConfig(&Config{})
		if !errors.Is(err, ErrAppKeyRequired) {
			t.Fatalf("expected ErrAppKeyRequired, got %v", err)
		}
	})

	t.Run("trim values and keep defaults", func(t *testing.T) {
		cfg, err := prepareConfig(&Config{
			AppKey:       " app-key ",
			MasterSecret: " secret ",
			Endpoint:     " https://msgapi.umeng.com ",
			AliasType:    " subject_id ",
			UserAgent:    " agent ",
		})
		if err != nil {
			t.Fatalf("prepareConfig() error = %v", err)
		}
		if cfg.AppKey != "app-key" || cfg.MasterSecret != "secret" || cfg.Endpoint != "https://msgapi.umeng.com" || cfg.AliasType != "subject_id" {
			t.Fatalf("prepareConfig() did not trim config: %+v", cfg)
		}
		if cfg.now == nil {
			t.Fatal("prepareConfig() did not populate clock")
		}
	})
}

func TestSendValidation(t *testing.T) {
	client, err := New(&Config{
		AppKey:       "app-key",
		MasterSecret: "secret",
		HTTPClient:   &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return successResponse(), nil })},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := client.Send(nil, &SendRequest{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.Send(context.Background(), nil); !errors.Is(err, ErrRequestRequired) {
		t.Fatalf("expected ErrRequestRequired, got %v", err)
	}
	if _, err := client.Send(context.Background(), &SendRequest{
		Platform: PlatformAndroid,
		Aliases:  []string{"u-1"},
		Body:     "hello",
	}); !errors.Is(err, ErrAndroidTitleRequired) {
		t.Fatalf("expected ErrAndroidTitleRequired, got %v", err)
	}
}

func TestSendBuildsAndroidAliasRequest(t *testing.T) {
	var capturedRequest *http.Request
	var capturedBody string

	client, err := New(&Config{
		AppKey:       "app-key",
		MasterSecret: "secret",
		Endpoint:     "https://msgapi.umeng.com",
		AliasType:    "subject_id",
		Production:   true,
		now: func() time.Time {
			return time.Unix(1711968000, 0).UTC()
		},
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				capturedRequest = req
				body, err := io.ReadAll(req.Body)
				if err != nil {
					return nil, err
				}
				capturedBody = string(body)
				return successResponse(), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := &SendRequest{
		Platform:    PlatformAndroid,
		Aliases:     []string{" subject-1 ", " ", "subject-2"},
		Title:       " 订单提醒 ",
		Body:        " 订单已送达 ",
		Description: " 描述 ",
		Extra: map[string]string{
			"link": " app://order/1 ",
		},
	}

	response, err := client.Send(context.Background(), request)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if response.Data.MsgID != "msg-1" || response.Data.TaskID != "task-1" {
		t.Fatalf("unexpected response: %+v", response)
	}
	if capturedRequest == nil {
		t.Fatal("expected request to be captured")
	}
	if got, want := capturedRequest.URL.Query().Get("sign"), calculateSign(http.MethodPost, "https://msgapi.umeng.com/api/send", []byte(capturedBody), "secret"); got != want {
		t.Fatalf("unexpected sign %q, want %q", got, want)
	}
	if !strings.Contains(capturedBody, `"alias":"subject-1,subject-2"`) {
		t.Fatalf("expected aliases in body, got %s", capturedBody)
	}
	if !strings.Contains(capturedBody, `"alias_type":"subject_id"`) {
		t.Fatalf("expected alias type in body, got %s", capturedBody)
	}
	if !strings.Contains(capturedBody, `"production_mode":"true"`) {
		t.Fatalf("expected production mode in body, got %s", capturedBody)
	}
	if !strings.Contains(capturedBody, `"title":"订单提醒"`) || !strings.Contains(capturedBody, `"text":"订单已送达"`) {
		t.Fatalf("expected notification body in payload, got %s", capturedBody)
	}
	if got, want := request.Title, " 订单提醒 "; got != want {
		t.Fatalf("original request title = %q, want %q", got, want)
	}
}

func TestSendBuildsIOSBroadcastRequest(t *testing.T) {
	var capturedBody string

	client, err := New(&Config{
		AppKey:       "app-key",
		MasterSecret: "secret",
		now: func() time.Time {
			return time.Unix(1711968000, 0).UTC()
		},
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					return nil, err
				}
				capturedBody = string(body)
				return successResponse(), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	badge := 3
	_, err = client.Send(context.Background(), &SendRequest{
		Platform:  PlatformIOS,
		Broadcast: true,
		Title:     "系统通知",
		Body:      "请查看最新消息",
		Badge:     &badge,
		Extra: map[string]string{
			"link": "app://message-center",
		},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !strings.Contains(capturedBody, `"type":"broadcast"`) {
		t.Fatalf("expected broadcast type, got %s", capturedBody)
	}
	if !strings.Contains(capturedBody, `"badge":3`) {
		t.Fatalf("expected badge in aps payload, got %s", capturedBody)
	}
	if !strings.Contains(capturedBody, `"sound":"default"`) {
		t.Fatalf("expected default sound in aps payload, got %s", capturedBody)
	}
	if !strings.Contains(capturedBody, `"title":"系统通知"`) || !strings.Contains(capturedBody, `"body":"请查看最新消息"`) {
		t.Fatalf("expected ios alert payload, got %s", capturedBody)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func successResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(`{"ret":"SUCCESS","data":{"msg_id":"msg-1","task_id":"task-1"}}`)),
	}
}
