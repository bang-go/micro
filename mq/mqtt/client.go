package mqtt

import (
	"fmt"

	"github.com/bang-go/util"
	pahomqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	AuthModeSignature = "Signature" //签名模式
	AuthModeToken     = "Token"     //Token模式
)

var defaultProtocolVersion uint = 4

type Config struct {
	ClientId              string
	Username              string
	Password              string
	AccessKeyId           string //如未设置username,则必填
	AccessKeySecret       string //如未设置password,则必填
	InstanceId            string //如未设置username,则必填
	Endpoint              string //tcp://foobar.com:1883
	GroupId               string //如未设置clientId,则必填
	DeviceId              string //如未设置clientId,则必填
	KeepAlive             int64
	ProtocolVersion       uint
	DefaultPublishHandler *pahomqtt.MessageHandler
	ConnectHandler        *pahomqtt.OnConnectHandler
	ReconnectHandler      *pahomqtt.ReconnectHandler
	ConnectLostHandler    *pahomqtt.ConnectionLostHandler
}
type MessageHandler = pahomqtt.MessageHandler
type Client interface {
	Disconnect(quiesce uint) //milliseconds
	Publish(topic string, qos byte, retained bool, payload interface{}) error
	Subscribe(topic string, qos byte, callback MessageHandler) error
	SubscribeMultiple(filters map[string]byte, callback MessageHandler) error
	Unsubscribe(topics ...string) error
	AddRoute(topic string, callback MessageHandler)
}
type clientEntity struct {
	mqttClient pahomqtt.Client
	*Config
}

func New(cfg *Config) (Client, error) {
	client := &clientEntity{}
	clientId := util.If(cfg.ClientId != "", cfg.ClientId, GetClientId(cfg.GroupId, cfg.DeviceId))
	username := util.If(cfg.Username != "", cfg.Username, GetUsername(AuthModeSignature, cfg.AccessKeyId, cfg.InstanceId))
	password := util.If(cfg.Password != "", cfg.Password, GetSignPassword(clientId, cfg.AccessKeySecret))
	if clientId == "" || username == "" || password == "" {
		return nil, fmt.Errorf("clientId or username or password is empty")
	}
	opts := pahomqtt.NewClientOptions()
	opts.AddBroker(cfg.Endpoint)
	opts.SetClientID(clientId)
	opts.SetUsername(username) //暂时只支持签名授权
	opts.SetPassword(password)
	var publishHandler = &defaultPublishHandler
	if cfg.DefaultPublishHandler != nil {
		publishHandler = cfg.DefaultPublishHandler
	}
	var connectHandler = &defaultConnectHandler
	if cfg.ConnectHandler != nil {
		connectHandler = cfg.ConnectHandler
	}
	var reconnectHandler = &defaultReconnectHandler
	if cfg.ReconnectHandler != nil {
		reconnectHandler = cfg.ReconnectHandler
	}
	var connectLostHandler = &defaultConnectLostHandler
	if cfg.ConnectLostHandler != nil {
		connectLostHandler = cfg.ConnectLostHandler
	}
	opts.SetDefaultPublishHandler(*publishHandler)
	opts.SetAutoReconnect(true)
	opts.OnConnect = *connectHandler
	opts.OnConnectionLost = *connectLostHandler
	opts.OnReconnecting = *reconnectHandler
	if cfg.KeepAlive > 0 {
		opts.KeepAlive = cfg.KeepAlive
	}
	//opts.SetProtocolVersion(util.If(cfg.ProtocolVersion > 0, cfg.ProtocolVersion, defaultProtocolVersion))
	client.mqttClient = pahomqtt.NewClient(opts)
	if token := client.mqttClient.Connect(); token.Wait() && token.Error() != nil {
		return client, token.Error()
	}
	return client, nil
}

func (s *clientEntity) Publish(topic string, qos byte, retained bool, payload interface{}) (err error) {
	if token := s.mqttClient.Publish(topic, qos, retained, payload); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return
}

func (s *clientEntity) Subscribe(topic string, qos byte, callback MessageHandler) (err error) {
	if token := s.mqttClient.Subscribe(topic, qos, callback); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return
}

func (s *clientEntity) SubscribeMultiple(filters map[string]byte, callback MessageHandler) (err error) {
	if token := s.mqttClient.SubscribeMultiple(filters, callback); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return
}

func (s *clientEntity) Unsubscribe(topics ...string) (err error) {
	if token := s.mqttClient.Unsubscribe(topics...); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return
}

func (s *clientEntity) Disconnect(quiesce uint) {
	s.mqttClient.Disconnect(quiesce)
}

func (s *clientEntity) AddRoute(topic string, callback MessageHandler) {
	s.mqttClient.AddRoute(topic, callback)
}

var defaultPublishHandler pahomqtt.MessageHandler = func(client pahomqtt.Client, msg pahomqtt.Message) {
	//fmt.Printf("Received message: %s from topic: %s\n", msg.Payload(), msg.Topic())
}

var defaultConnectHandler pahomqtt.OnConnectHandler = func(client pahomqtt.Client) {
	//fmt.Println("Connected")
}
var defaultReconnectHandler pahomqtt.ReconnectHandler = func(client pahomqtt.Client, options *pahomqtt.ClientOptions) {
	//fmt.Println("Reconnected")

}
var defaultConnectLostHandler pahomqtt.ConnectionLostHandler = func(client pahomqtt.Client, err error) {
	//fmt.Printf("Connect lost: %v", err)
}
