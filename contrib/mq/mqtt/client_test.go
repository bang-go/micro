package mqtt

import (
	"context"
	"errors"
	"testing"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
)

func TestPrepareConfigValidation(t *testing.T) {
	_, _, err := prepareConfig(nil)
	if !errors.Is(err, ErrNilConfig) {
		t.Fatalf("prepareConfig(nil) error = %v, want %v", err, ErrNilConfig)
	}

	_, _, err = prepareConfig(&Config{})
	if !errors.Is(err, ErrBrokerRequired) {
		t.Fatalf("prepareConfig(no brokers) error = %v, want %v", err, ErrBrokerRequired)
	}
}

func TestPrepareConfigNormalizesAndClonesInput(t *testing.T) {
	conf := &Config{
		Brokers:  []string{" tcp://localhost:1883 ", "tcp://localhost:1883", ""},
		ClientID: " client ",
		Username: " user ",
		Password: " pass ",
		Aliyun: &AliyunAuth{
			Mode:            " signature ",
			AccessKeyID:     " ak ",
			AccessKeySecret: " secret ",
			InstanceID:      " instance ",
			GroupID:         " group ",
			DeviceID:        " device ",
		},
	}

	normalized, options, err := prepareConfig(conf)
	if err != nil {
		t.Fatalf("prepareConfig() error = %v", err)
	}

	if got, want := len(normalized.Brokers), 1; got != want {
		t.Fatalf("len(Brokers) = %d, want %d", got, want)
	}
	if got, want := normalized.Brokers[0], "tcp://localhost:1883"; got != want {
		t.Fatalf("Brokers[0] = %q, want %q", got, want)
	}
	if got, want := normalized.ClientID, "client"; got != want {
		t.Fatalf("ClientID = %q, want %q", got, want)
	}
	if got, want := normalized.Username, "user"; got != want {
		t.Fatalf("Username = %q, want %q", got, want)
	}
	if got, want := normalized.Password, "pass"; got != want {
		t.Fatalf("Password = %q, want %q", got, want)
	}
	if normalized.Aliyun == nil {
		t.Fatal("Aliyun = nil")
	}
	if got, want := normalized.Aliyun.Mode, AuthModeSignature; got != want {
		t.Fatalf("Aliyun.Mode = %q, want %q", got, want)
	}

	reader := optionsReader(options)
	if got, want := reader.ClientID(), "client"; got != want {
		t.Fatalf("options.ClientID = %q, want %q", got, want)
	}
	if got, want := reader.Username(), "user"; got != want {
		t.Fatalf("options.Username = %q, want %q", got, want)
	}
	if got, want := reader.Password(), "pass"; got != want {
		t.Fatalf("options.Password = %q, want %q", got, want)
	}

	conf.Brokers[0] = "mutated"
	conf.ClientID = "mutated"
	conf.Username = "mutated"
	conf.Password = "mutated"
	conf.Aliyun.Mode = "mutated"
	if got, want := normalized.Brokers[0], "tcp://localhost:1883"; got != want {
		t.Fatalf("normalized.Brokers[0] = %q, want %q", got, want)
	}
	if got, want := normalized.ClientID, "client"; got != want {
		t.Fatalf("normalized.ClientID = %q, want %q", got, want)
	}
	if got, want := normalized.Aliyun.Mode, AuthModeSignature; got != want {
		t.Fatalf("normalized.Aliyun.Mode = %q, want %q", got, want)
	}
}

func TestPrepareConfigRejectsInvalidAliyunMode(t *testing.T) {
	_, _, err := prepareConfig(&Config{
		Brokers: []string{"tcp://localhost:1883"},
		Aliyun: &AliyunAuth{
			Mode:            "invalid",
			AccessKeyID:     "ak",
			AccessKeySecret: "secret",
			InstanceID:      "instance",
			GroupID:         "group",
			DeviceID:        "device",
		},
	})
	if !errors.Is(err, ErrInvalidAliyunAuthMode) {
		t.Fatalf("prepareConfig() error = %v, want %v", err, ErrInvalidAliyunAuthMode)
	}
}

func TestPrepareConfigUsesAliyunDerivedCredentials(t *testing.T) {
	conf, options, err := prepareConfig(&Config{
		Brokers: []string{"tcp://localhost:1883"},
		Aliyun: &AliyunAuth{
			AccessKeyID:     "ak",
			AccessKeySecret: "secret",
			InstanceID:      "instance",
			GroupID:         "group",
			DeviceID:        "device",
		},
	})
	if err != nil {
		t.Fatalf("prepareConfig() error = %v", err)
	}

	if conf == nil {
		t.Fatal("prepareConfig() config = nil")
	}

	reader := optionsReader(options)
	if got, want := reader.ClientID(), BuildClientID("group", "device"); got != want {
		t.Fatalf("ClientID = %q, want %q", got, want)
	}
	if got, want := reader.Username(), BuildUsername(AuthModeSignature, "ak", "instance"); got != want {
		t.Fatalf("Username = %q, want %q", got, want)
	}
	if got, want := reader.Password(), BuildSignaturePassword(BuildClientID("group", "device"), "secret"); got != want {
		t.Fatalf("Password = %q, want %q", got, want)
	}
}

func TestOpenConnectsAndUsesFactory(t *testing.T) {
	fake := &fakeMQTTClient{connectToken: newFakeToken(nil)}
	var captured *pahomqtt.ClientOptions

	client, err := Open(context.Background(), &Config{
		Brokers:  []string{"tcp://localhost:1883"},
		ClientID: "client",
		Username: "user",
		Password: "pass",
		newClient: func(options *pahomqtt.ClientOptions) pahomqtt.Client {
			captured = options
			return fake
		},
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if client == nil {
		t.Fatal("Open() client = nil")
	}
	if captured == nil {
		t.Fatal("newClient was not invoked")
	}
}

func TestOpenRejectsNilContext(t *testing.T) {
	_, err := Open(nil, &Config{
		Brokers:  []string{"tcp://localhost:1883"},
		ClientID: "client",
		Username: "user",
		Password: "pass",
	})
	if !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Open(nil, ...) error = %v, want %v", err, ErrContextRequired)
	}
}

func TestOpenRespectsContextTimeout(t *testing.T) {
	fake := &fakeMQTTClient{connectToken: newDelayedToken(50 * time.Millisecond)}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := Open(ctx, &Config{
		Brokers:  []string{"tcp://localhost:1883"},
		ClientID: "client",
		Username: "user",
		Password: "pass",
		newClient: func(options *pahomqtt.ClientOptions) pahomqtt.Client {
			return fake
		},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Open() error = %v, want deadline exceeded", err)
	}
}

func TestClientOperations(t *testing.T) {
	fake := &fakeMQTTClient{
		connectToken:     newFakeToken(nil),
		publishToken:     newFakeToken(nil),
		subscribeToken:   newFakeToken(nil),
		subscribeManyTok: newFakeToken(nil),
		unsubscribeToken: newFakeToken(nil),
	}
	client, err := Open(context.Background(), &Config{
		Brokers:  []string{"tcp://localhost:1883"},
		ClientID: "client",
		Username: "user",
		Password: "pass",
		newClient: func(options *pahomqtt.ClientOptions) pahomqtt.Client {
			return fake
		},
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := client.Publish(context.Background(), "topic/a", 1, false, "payload"); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err := client.Subscribe(context.Background(), "topic/a", 1, nil); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if err := client.SubscribeMultiple(context.Background(), map[string]byte{"topic/a": 1}, nil); err != nil {
		t.Fatalf("SubscribeMultiple() error = %v", err)
	}
	if err := client.Unsubscribe(context.Background(), "topic/a"); err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
	if err := client.AddRoute("topic/a", nil); err != nil {
		t.Fatalf("AddRoute() error = %v", err)
	}

	client.Disconnect(1000)

	if fake.lastPublishedTopic != "topic/a" {
		t.Fatalf("lastPublishedTopic = %q, want topic/a", fake.lastPublishedTopic)
	}
	if !fake.disconnected {
		t.Fatal("Disconnect() was not forwarded")
	}
	if got, want := fake.lastSubscribeTopic, "topic/a"; got != want {
		t.Fatalf("lastSubscribeTopic = %q, want %q", got, want)
	}
	if got, want := fake.lastAddRouteTopic, "topic/a"; got != want {
		t.Fatalf("lastAddRouteTopic = %q, want %q", got, want)
	}
	if got, want := len(fake.lastSubscribeMany), 1; got != want {
		t.Fatalf("len(lastSubscribeMany) = %d, want %d", got, want)
	}
	if got, want := fake.lastSubscribeMany["topic/a"], byte(1); got != want {
		t.Fatalf("lastSubscribeMany[topic/a] = %d, want %d", got, want)
	}
	if got, want := len(fake.lastUnsubscribeTopics), 1; got != want {
		t.Fatalf("len(lastUnsubscribeTopics) = %d, want %d", got, want)
	}
	if got, want := fake.lastUnsubscribeTopics[0], "topic/a"; got != want {
		t.Fatalf("lastUnsubscribeTopics[0] = %q, want %q", got, want)
	}
}

func TestClientValidation(t *testing.T) {
	client := &clientEntity{
		client:        &fakeMQTTClient{},
		operationWait: time.Second,
	}

	if err := client.Publish(context.Background(), "", 0, false, nil); !errors.Is(err, ErrTopicRequired) {
		t.Fatalf("Publish() error = %v, want %v", err, ErrTopicRequired)
	}
	if err := client.Subscribe(context.Background(), "", 0, nil); !errors.Is(err, ErrTopicRequired) {
		t.Fatalf("Subscribe() error = %v, want %v", err, ErrTopicRequired)
	}
	if err := client.SubscribeMultiple(context.Background(), nil, nil); !errors.Is(err, ErrFiltersRequired) {
		t.Fatalf("SubscribeMultiple() error = %v, want %v", err, ErrFiltersRequired)
	}
	if err := client.Unsubscribe(context.Background()); !errors.Is(err, ErrNoTopics) {
		t.Fatalf("Unsubscribe() error = %v, want %v", err, ErrNoTopics)
	}
	if err := client.AddRoute("", nil); !errors.Is(err, ErrTopicRequired) {
		t.Fatalf("AddRoute() error = %v, want %v", err, ErrTopicRequired)
	}
}

func TestClientRejectsNilContext(t *testing.T) {
	client := &clientEntity{
		client:        &fakeMQTTClient{},
		operationWait: time.Second,
	}

	if err := client.Publish(nil, "topic/a", 0, false, nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Publish(nil, ...) error = %v, want %v", err, ErrContextRequired)
	}
	if err := client.Subscribe(nil, "topic/a", 0, nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Subscribe(nil, ...) error = %v, want %v", err, ErrContextRequired)
	}
	if err := client.SubscribeMultiple(nil, map[string]byte{"topic/a": 0}, nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("SubscribeMultiple(nil, ...) error = %v, want %v", err, ErrContextRequired)
	}
	if err := client.Unsubscribe(nil, "topic/a"); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Unsubscribe(nil, ...) error = %v, want %v", err, ErrContextRequired)
	}
}

func TestClientNormalizesTopicsAndFilters(t *testing.T) {
	fake := &fakeMQTTClient{
		publishToken:     newFakeToken(nil),
		subscribeToken:   newFakeToken(nil),
		subscribeManyTok: newFakeToken(nil),
		unsubscribeToken: newFakeToken(nil),
	}
	client := &clientEntity{
		client:        fake,
		operationWait: time.Second,
	}

	if err := client.Publish(context.Background(), " topic/a ", 1, false, "payload"); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err := client.Subscribe(context.Background(), " topic/a ", 1, nil); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if err := client.SubscribeMultiple(context.Background(), map[string]byte{" topic/a ": 1}, nil); err != nil {
		t.Fatalf("SubscribeMultiple() error = %v", err)
	}
	if err := client.Unsubscribe(context.Background(), " topic/a ", "topic/a"); err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
	if err := client.AddRoute(" topic/a ", nil); err != nil {
		t.Fatalf("AddRoute() error = %v", err)
	}

	if got, want := fake.lastPublishedTopic, "topic/a"; got != want {
		t.Fatalf("lastPublishedTopic = %q, want %q", got, want)
	}
	if got, want := fake.lastSubscribeTopic, "topic/a"; got != want {
		t.Fatalf("lastSubscribeTopic = %q, want %q", got, want)
	}
	if got, want := len(fake.lastSubscribeMany), 1; got != want {
		t.Fatalf("len(lastSubscribeMany) = %d, want %d", got, want)
	}
	if got, want := fake.lastSubscribeMany["topic/a"], byte(1); got != want {
		t.Fatalf("lastSubscribeMany[topic/a] = %d, want %d", got, want)
	}
	if got, want := len(fake.lastUnsubscribeTopics), 1; got != want {
		t.Fatalf("len(lastUnsubscribeTopics) = %d, want %d", got, want)
	}
	if got, want := fake.lastUnsubscribeTopics[0], "topic/a"; got != want {
		t.Fatalf("lastUnsubscribeTopics[0] = %q, want %q", got, want)
	}
	if got, want := fake.lastAddRouteTopic, "topic/a"; got != want {
		t.Fatalf("lastAddRouteTopic = %q, want %q", got, want)
	}
}

func TestClientRejectsInvalidNormalizedTopicsAndFilters(t *testing.T) {
	client := &clientEntity{
		client:        &fakeMQTTClient{},
		operationWait: time.Second,
	}

	if err := client.Publish(context.Background(), " ", 0, false, nil); !errors.Is(err, ErrTopicRequired) {
		t.Fatalf("Publish(blank topic) error = %v, want %v", err, ErrTopicRequired)
	}
	if err := client.Subscribe(context.Background(), " ", 0, nil); !errors.Is(err, ErrTopicRequired) {
		t.Fatalf("Subscribe(blank topic) error = %v, want %v", err, ErrTopicRequired)
	}
	if err := client.SubscribeMultiple(context.Background(), map[string]byte{" ": 0}, nil); !errors.Is(err, ErrTopicRequired) {
		t.Fatalf("SubscribeMultiple(blank filter) error = %v, want %v", err, ErrTopicRequired)
	}
	if err := client.SubscribeMultiple(context.Background(), map[string]byte{"topic/a": 0, " topic/a ": 1}, nil); !errors.Is(err, ErrDuplicateFilterTopic) {
		t.Fatalf("SubscribeMultiple(duplicate normalized filters) error = %v, want %v", err, ErrDuplicateFilterTopic)
	}
	if err := client.Unsubscribe(context.Background(), "topic/a", " "); !errors.Is(err, ErrTopicRequired) {
		t.Fatalf("Unsubscribe(blank topic) error = %v, want %v", err, ErrTopicRequired)
	}
	if err := client.AddRoute(" ", nil); !errors.Is(err, ErrTopicRequired) {
		t.Fatalf("AddRoute(blank topic) error = %v, want %v", err, ErrTopicRequired)
	}
}

func TestWaitTokenRejectsNilToken(t *testing.T) {
	err := waitToken(context.Background(), time.Second, nil)
	if !errors.Is(err, ErrOperationTokenRequired) {
		t.Fatalf("waitToken(nil token) error = %v, want %v", err, ErrOperationTokenRequired)
	}
}

func optionsReader(options *pahomqtt.ClientOptions) pahomqtt.ClientOptionsReader {
	return pahomqtt.NewOptionsReader(options)
}

type fakeMQTTClient struct {
	connectToken     pahomqtt.Token
	publishToken     pahomqtt.Token
	subscribeToken   pahomqtt.Token
	subscribeManyTok pahomqtt.Token
	unsubscribeToken pahomqtt.Token

	lastPublishedTopic    string
	lastSubscribeTopic    string
	lastAddRouteTopic     string
	lastSubscribeMany     map[string]byte
	lastUnsubscribeTopics []string
	disconnected          bool
}

func (f *fakeMQTTClient) IsConnected() bool {
	return true
}

func (f *fakeMQTTClient) IsConnectionOpen() bool {
	return true
}

func (f *fakeMQTTClient) Connect() pahomqtt.Token {
	return f.connectToken
}

func (f *fakeMQTTClient) Disconnect(quiesce uint) {
	f.disconnected = true
}

func (f *fakeMQTTClient) Publish(topic string, qos byte, retained bool, payload interface{}) pahomqtt.Token {
	f.lastPublishedTopic = topic
	return f.publishToken
}

func (f *fakeMQTTClient) Subscribe(topic string, qos byte, callback pahomqtt.MessageHandler) pahomqtt.Token {
	f.lastSubscribeTopic = topic
	return f.subscribeToken
}

func (f *fakeMQTTClient) SubscribeMultiple(filters map[string]byte, callback pahomqtt.MessageHandler) pahomqtt.Token {
	f.lastSubscribeMany = make(map[string]byte, len(filters))
	for topic, qos := range filters {
		f.lastSubscribeMany[topic] = qos
	}
	return f.subscribeManyTok
}

func (f *fakeMQTTClient) Unsubscribe(topics ...string) pahomqtt.Token {
	f.lastUnsubscribeTopics = append([]string(nil), topics...)
	return f.unsubscribeToken
}

func (f *fakeMQTTClient) AddRoute(topic string, callback pahomqtt.MessageHandler) {
	f.lastAddRouteTopic = topic
}

func (f *fakeMQTTClient) OptionsReader() pahomqtt.ClientOptionsReader {
	return pahomqtt.NewOptionsReader(pahomqtt.NewClientOptions())
}

type fakeToken struct {
	done chan struct{}
	err  error
}

func newFakeToken(err error) *fakeToken {
	done := make(chan struct{})
	close(done)
	return &fakeToken{done: done, err: err}
}

func newDelayedToken(delay time.Duration) *fakeToken {
	done := make(chan struct{})
	go func() {
		time.Sleep(delay)
		close(done)
	}()
	return &fakeToken{done: done}
}

func (t *fakeToken) Wait() bool {
	<-t.done
	return true
}

func (t *fakeToken) WaitTimeout(timeout time.Duration) bool {
	select {
	case <-t.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (t *fakeToken) Done() <-chan struct{} {
	return t.done
}

func (t *fakeToken) Error() error {
	return t.err
}
