package mqtt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	AuthModeSignature = "Signature"
	AuthModeToken     = "Token"
)

const (
	defaultProtocolVersion = 4
	defaultConnectTimeout  = 30 * time.Second
	defaultOperationWait   = 30 * time.Second
)

type AliyunAuth struct {
	Mode            string
	AccessKeyID     string
	AccessKeySecret string
	InstanceID      string
	GroupID         string
	DeviceID        string
}

type Config struct {
	Brokers []string

	ClientID string
	Username string
	Password string

	Aliyun *AliyunAuth

	KeepAlive       time.Duration
	ConnectTimeout  time.Duration
	OperationWait   time.Duration
	ProtocolVersion uint
	AutoReconnect   bool
	CleanSession    bool
	OrderMatters    bool

	DefaultPublishHandler pahomqtt.MessageHandler
	OnConnect             pahomqtt.OnConnectHandler
	OnReconnect           pahomqtt.ReconnectHandler
	OnConnectionLost      pahomqtt.ConnectionLostHandler

	newClient func(*pahomqtt.ClientOptions) pahomqtt.Client
}

type MessageHandler = pahomqtt.MessageHandler

type Client interface {
	Raw() pahomqtt.Client
	IsConnected() bool
	Disconnect(quiesce uint)
	Publish(ctx context.Context, topic string, qos byte, retained bool, payload any) error
	Subscribe(ctx context.Context, topic string, qos byte, callback MessageHandler) error
	SubscribeMultiple(ctx context.Context, filters map[string]byte, callback MessageHandler) error
	Unsubscribe(ctx context.Context, topics ...string) error
	AddRoute(topic string, callback MessageHandler) error
}

type clientEntity struct {
	client        pahomqtt.Client
	operationWait time.Duration
}

func Open(ctx context.Context, conf *Config) (Client, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}

	config, options, err := prepareConfig(conf)
	if err != nil {
		return nil, err
	}

	factory := config.newClient
	if factory == nil {
		factory = pahomqtt.NewClient
	}

	client := factory(options)
	if err := waitToken(ctx, config.ConnectTimeout, client.Connect()); err != nil {
		return nil, fmt.Errorf("mqtt: connect failed: %w", err)
	}

	return &clientEntity{
		client:        client,
		operationWait: config.OperationWait,
	}, nil
}

func New(conf *Config) (Client, error) {
	return Open(context.Background(), conf)
}

func (c *clientEntity) Raw() pahomqtt.Client {
	return c.client
}

func (c *clientEntity) IsConnected() bool {
	return c.client.IsConnected()
}

func (c *clientEntity) Disconnect(quiesce uint) {
	c.client.Disconnect(quiesce)
}

func (c *clientEntity) Publish(ctx context.Context, topic string, qos byte, retained bool, payload any) error {
	if ctx == nil {
		return ErrContextRequired
	}

	topic = normalizeTopic(topic)
	if topic == "" {
		return ErrTopicRequired
	}
	return waitToken(ctx, c.operationWait, c.client.Publish(topic, qos, retained, payload))
}

func (c *clientEntity) Subscribe(ctx context.Context, topic string, qos byte, callback MessageHandler) error {
	if ctx == nil {
		return ErrContextRequired
	}

	topic = normalizeTopic(topic)
	if topic == "" {
		return ErrTopicRequired
	}
	return waitToken(ctx, c.operationWait, c.client.Subscribe(topic, qos, callback))
}

func (c *clientEntity) SubscribeMultiple(ctx context.Context, filters map[string]byte, callback MessageHandler) error {
	if ctx == nil {
		return ErrContextRequired
	}

	normalized, err := normalizeFilters(filters)
	if err != nil {
		return err
	}
	return waitToken(ctx, c.operationWait, c.client.SubscribeMultiple(normalized, callback))
}

func (c *clientEntity) Unsubscribe(ctx context.Context, topics ...string) error {
	if ctx == nil {
		return ErrContextRequired
	}

	normalized, err := normalizeTopics(topics)
	if err != nil {
		return err
	}
	return waitToken(ctx, c.operationWait, c.client.Unsubscribe(normalized...))
}

func (c *clientEntity) AddRoute(topic string, callback MessageHandler) error {
	topic = normalizeTopic(topic)
	if topic == "" {
		return ErrTopicRequired
	}
	c.client.AddRoute(topic, callback)
	return nil
}

func prepareConfig(conf *Config) (*Config, *pahomqtt.ClientOptions, error) {
	if conf == nil {
		return nil, nil, ErrNilConfig
	}

	cloned := *conf
	cloned.Brokers = trimNonEmpty(conf.Brokers)
	cloned.ClientID = strings.TrimSpace(cloned.ClientID)
	cloned.Username = strings.TrimSpace(cloned.Username)
	cloned.Password = strings.TrimSpace(cloned.Password)
	aliyun, err := normalizeAliyunAuth(conf.Aliyun)
	if err != nil {
		return nil, nil, err
	}
	cloned.Aliyun = aliyun
	if len(cloned.Brokers) == 0 {
		return nil, nil, ErrBrokerRequired
	}

	if cloned.ConnectTimeout <= 0 {
		cloned.ConnectTimeout = defaultConnectTimeout
	}
	if cloned.OperationWait <= 0 {
		cloned.OperationWait = defaultOperationWait
	}
	if cloned.ProtocolVersion == 0 {
		cloned.ProtocolVersion = defaultProtocolVersion
	}

	clientID, username, password, err := resolveCredentials(&cloned)
	if err != nil {
		return nil, nil, err
	}

	options := pahomqtt.NewClientOptions()
	for _, broker := range cloned.Brokers {
		options.AddBroker(broker)
	}
	options.SetClientID(clientID)
	options.SetUsername(username)
	options.SetPassword(password)
	options.SetProtocolVersion(cloned.ProtocolVersion)
	options.SetAutoReconnect(cloned.AutoReconnect)
	options.SetCleanSession(cloned.CleanSession)
	options.SetOrderMatters(cloned.OrderMatters)
	options.SetConnectTimeout(cloned.ConnectTimeout)

	if cloned.KeepAlive > 0 {
		options.SetKeepAlive(cloned.KeepAlive)
	}
	if cloned.DefaultPublishHandler != nil {
		options.SetDefaultPublishHandler(cloned.DefaultPublishHandler)
	}
	if cloned.OnConnect != nil {
		options.OnConnect = cloned.OnConnect
	}
	if cloned.OnReconnect != nil {
		options.OnReconnecting = cloned.OnReconnect
	}
	if cloned.OnConnectionLost != nil {
		options.OnConnectionLost = cloned.OnConnectionLost
	}

	return &cloned, options, nil
}

func resolveCredentials(conf *Config) (string, string, string, error) {
	clientID := conf.ClientID
	username := conf.Username
	password := conf.Password

	if conf.Aliyun != nil {
		auth := *conf.Aliyun
		if auth.Mode == "" {
			auth.Mode = AuthModeSignature
		}
		if clientID == "" && auth.GroupID != "" && auth.DeviceID != "" {
			clientID = BuildClientID(auth.GroupID, auth.DeviceID)
		}
		if username == "" && auth.AccessKeyID != "" && auth.InstanceID != "" {
			username = BuildUsername(auth.Mode, auth.AccessKeyID, auth.InstanceID)
		}
		if password == "" && clientID != "" && auth.AccessKeySecret != "" {
			password = BuildSignaturePassword(clientID, auth.AccessKeySecret)
		}
	}

	switch {
	case clientID == "":
		return "", "", "", ErrClientIDRequired
	case username == "":
		return "", "", "", ErrUsernameRequired
	case password == "":
		return "", "", "", ErrPasswordRequired
	default:
		return clientID, username, password, nil
	}
}

func IsTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}
