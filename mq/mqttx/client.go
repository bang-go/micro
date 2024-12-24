package mqttx

import (
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	AuthModeSignature = "Signature" //签名模式
	AuthModeToken     = "Token"     //Token模式
)

var defaultProtocolVersion uint = 4

type Config struct {
	AccessKeyId           string
	AccessKeySecret       string
	InstanceId            string
	Endpoint              string //tcp://foobar.com:1883
	GroupId               string
	DeviceId              string //设备id
	ProtocolVersion       uint
	DefaultPublishHandler *mqtt.MessageHandler
	ConnectHandler        *mqtt.OnConnectHandler
	ReconnectHandler      *mqtt.ReconnectHandler
	ConnectLostHandler    *mqtt.ConnectionLostHandler
}
type MessageHandler = mqtt.MessageHandler
type Client interface {
	Disconnect(quiesce uint) //milliseconds
	Publish(topic string, qos byte, retained bool, payload interface{})
	Subscribe(topic string, qos byte, callback MessageHandler)
	SubscribeMultiple(filters map[string]byte, callback MessageHandler)
	Unsubscribe(topics ...string)
	AddRoute(topic string, callback MessageHandler)
}
type clientEntity struct {
	mqttClient mqtt.Client
	*Config
}

func New(cfg *Config) (Client, error) {
	client := &clientEntity{}
	clientId := GetClientId(cfg.GroupId, cfg.DeviceId)
	opts := mqtt.NewClientOptions()
	opts.AddBroker(cfg.Endpoint)
	opts.SetClientID(clientId)
	opts.SetUsername(GetUserName(AuthModeSignature, cfg.AccessKeyId, cfg.InstanceId)) //暂时只支持签名授权
	opts.SetPassword(GetSignPassword(clientId, cfg.AccessKeySecret))
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
	//opts.SetProtocolVersion(util.If(cfg.ProtocolVersion > 0, cfg.ProtocolVersion, defaultProtocolVersion))
	client.mqttClient = mqtt.NewClient(opts)
	if token := client.mqttClient.Connect(); token.Wait() && token.Error() != nil {
		return client, token.Error()
	}
	return client, nil
}

func (s *clientEntity) Publish(topic string, qos byte, retained bool, payload interface{}) {
	token := s.mqttClient.Publish(topic, qos, retained, payload)
	token.Wait()
}

func (s *clientEntity) Subscribe(topic string, qos byte, callback MessageHandler) {
	token := s.mqttClient.Subscribe(topic, qos, callback)
	token.Wait()
}

func (s *clientEntity) SubscribeMultiple(filters map[string]byte, callback MessageHandler) {
	token := s.mqttClient.SubscribeMultiple(filters, callback)
	token.Wait()
}

func (s *clientEntity) Unsubscribe(topics ...string) {
	token := s.mqttClient.Unsubscribe(topics...)
	token.Wait()
}

func (s *clientEntity) Disconnect(quiesce uint) {
	s.mqttClient.Disconnect(quiesce)
}

func (s *clientEntity) AddRoute(topic string, callback MessageHandler) {
	s.mqttClient.AddRoute(topic, callback)
}

var defaultPublishHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("Received message: %s from topic: %s\n", msg.Payload(), msg.Topic())
}

var defaultConnectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	fmt.Println("Connected")
}
var defaultReconnectHandler mqtt.ReconnectHandler = func(client mqtt.Client, options *mqtt.ClientOptions) {
	fmt.Println("Reconnected")

}
var defaultConnectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	fmt.Printf("Connect lost: %v", err)
}
