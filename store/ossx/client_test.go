package ossx

import (
	"context"
	"errors"
	"net/http"
	"testing"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

func TestPrepareConfig(t *testing.T) {
	t.Run("validate config", func(t *testing.T) {
		_, err := prepareConfig(nil)
		if !errors.Is(err, ErrNilConfig) {
			t.Fatalf("expected ErrNilConfig, got %v", err)
		}

		_, err = prepareConfig(&Config{})
		if !errors.Is(err, ErrEndpointRequired) {
			t.Fatalf("expected ErrEndpointRequired, got %v", err)
		}
	})

	t.Run("build from explicit values", func(t *testing.T) {
		base := aliyunoss.NewConfig()
		base.WithRegion("mutated")
		cfg, err := prepareConfig(&Config{
			Endpoint:        " oss-cn-hangzhou.aliyuncs.com ",
			Region:          " cn-hangzhou ",
			AccessKeyID:     " ak ",
			AccessKeySecret: " sk ",
			Base:            base,
		})
		if err != nil {
			t.Fatalf("prepareConfig() error = %v", err)
		}
		sdk := buildSDKConfig(cfg)
		if sdk.Endpoint == nil || *sdk.Endpoint != "oss-cn-hangzhou.aliyuncs.com" || sdk.Region == nil || *sdk.Region != "cn-hangzhou" {
			t.Fatalf("unexpected sdk config: %+v", sdk)
		}
		if sdk.CredentialsProvider == nil {
			t.Fatal("expected credentials provider to be configured")
		}
		base.WithEndpoint("mutated")
		if cfg.Base == base {
			t.Fatal("expected base config to be cloned")
		}
	})
}

func TestNewAndOperations(t *testing.T) {
	fake := &fakeOSSAPI{}
	client, err := New(&Config{
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		Region:          "cn-hangzhou",
		AccessKeyID:     "ak",
		AccessKeySecret: "sk",
		newClient: func(*aliyunoss.Config, ...func(*Options)) ossAPI {
			return fake
		},
		HTTPClient: http.DefaultClient,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if client.Raw() != nil {
		t.Fatal("expected Raw() to be nil when fake api is injected")
	}

	ctx := context.WithValue(context.Background(), testContextKey("trace"), "value")
	if _, err := client.PutObject(ctx, &PutObjectRequest{}); err != nil {
		t.Fatalf("PutObject() error = %v", err)
	}
	if fake.ctxValue != "value" {
		t.Fatalf("expected context to be forwarded, got %q", fake.ctxValue)
	}

	if _, err := client.PutObjectFromFile(context.Background(), &PutObjectRequest{}, " /tmp/file "); err != nil {
		t.Fatalf("PutObjectFromFile() error = %v", err)
	}
	if fake.filePath != "/tmp/file" {
		t.Fatalf("expected file path to be forwarded, got %q", fake.filePath)
	}

	if _, err := client.AppendObject(context.Background(), &AppendObjectRequest{}); err != nil {
		t.Fatalf("AppendObject() error = %v", err)
	}
	if _, err := client.AppendFile(context.Background(), " bucket ", " key "); err != nil {
		t.Fatalf("AppendFile() error = %v", err)
	}
	if fake.bucket != "bucket" || fake.key != "key" {
		t.Fatalf("unexpected append file args: %+v", fake)
	}
}

func TestValidation(t *testing.T) {
	fake := &fakeOSSAPI{}
	client, err := New(&Config{
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		Region:          "cn-hangzhou",
		AccessKeyID:     "ak",
		AccessKeySecret: "sk",
		newClient: func(*aliyunoss.Config, ...func(*Options)) ossAPI {
			return fake
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := client.PutObject(nil, &PutObjectRequest{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.PutObject(context.Background(), nil); !errors.Is(err, ErrRequestRequired) {
		t.Fatalf("expected ErrRequestRequired, got %v", err)
	}
	if _, err := client.PutObjectFromFile(nil, &PutObjectRequest{}, "/tmp/file"); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.PutObjectFromFile(context.Background(), &PutObjectRequest{}, " "); !errors.Is(err, ErrFilePathRequired) {
		t.Fatalf("expected ErrFilePathRequired, got %v", err)
	}
	if _, err := client.AppendObject(nil, &AppendObjectRequest{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.AppendObject(context.Background(), nil); !errors.Is(err, ErrRequestRequired) {
		t.Fatalf("expected ErrRequestRequired, got %v", err)
	}
	if _, err := client.AppendFile(nil, "bucket", "key"); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.AppendFile(context.Background(), " ", "key"); !errors.Is(err, ErrBucketRequired) {
		t.Fatalf("expected ErrBucketRequired, got %v", err)
	}
	if _, err := client.AppendFile(context.Background(), "bucket", " "); !errors.Is(err, ErrKeyRequired) {
		t.Fatalf("expected ErrKeyRequired, got %v", err)
	}
}

type testContextKey string

type fakeOSSAPI struct {
	ctxValue string
	filePath string
	bucket   string
	key      string
}

func (f *fakeOSSAPI) PutObject(ctx context.Context, _ *PutObjectRequest, _ ...func(*Options)) (*PutObjectResult, error) {
	if value, _ := ctx.Value(testContextKey("trace")).(string); value != "" {
		f.ctxValue = value
	}
	return &PutObjectResult{}, nil
}

func (f *fakeOSSAPI) PutObjectFromFile(_ context.Context, _ *PutObjectRequest, filePath string, _ ...func(*Options)) (*PutObjectResult, error) {
	f.filePath = filePath
	return &PutObjectResult{}, nil
}

func (f *fakeOSSAPI) AppendObject(context.Context, *AppendObjectRequest, ...func(*Options)) (*AppendObjectResult, error) {
	return &AppendObjectResult{}, nil
}

func (f *fakeOSSAPI) AppendFile(_ context.Context, bucket, key string, _ ...func(*AppendOptions)) (*AppendOnlyFile, error) {
	f.bucket = bucket
	f.key = key
	return &AppendOnlyFile{}, nil
}
