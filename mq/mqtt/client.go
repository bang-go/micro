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

// New 创建新的 MQTT 客户端
// cfg: MQTT 配置
// 返回: Client 实例和错误
func New(cfg *Config) (Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("Config 不能为 nil")
	}
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("Endpoint 不能为空")
	}

	client := &clientEntity{}
	clientId := util.If(cfg.ClientId != "", cfg.ClientId, GetClientId(cfg.GroupId, cfg.DeviceId))
	username := util.If(cfg.Username != "", cfg.Username, GetUsername(AuthModeSignature, cfg.AccessKeyId, cfg.InstanceId))
	password := util.If(cfg.Password != "", cfg.Password, GetSignPassword(clientId, cfg.AccessKeySecret))

	if clientId == "" {
		return nil, fmt.Errorf("clientId 不能为空，请设置 ClientId 或 GroupId+DeviceId")
	}
	if username == "" {
		return nil, fmt.Errorf("username 不能为空，请设置 Username 或 AccessKeyId+InstanceId")
	}
	if password == "" {
		return nil, fmt.Errorf("password 不能为空，请设置 Password 或 AccessKeySecret")
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

// Publish 发布消息到指定主题
func (s *clientEntity) Publish(topic string, qos byte, retained bool, payload interface{}) error {
	if token := s.mqttClient.Publish(topic, qos, retained, payload); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

// Subscribe 订阅指定主题
func (s *clientEntity) Subscribe(topic string, qos byte, callback MessageHandler) error {
	if token := s.mqttClient.Subscribe(topic, qos, callback); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

// SubscribeMultiple 订阅多个主题
func (s *clientEntity) SubscribeMultiple(filters map[string]byte, callback MessageHandler) error {
	if token := s.mqttClient.SubscribeMultiple(filters, callback); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

// Unsubscribe 取消订阅主题
func (s *clientEntity) Unsubscribe(topics ...string) error {
	if token := s.mqttClient.Unsubscribe(topics...); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

// Disconnect 断开连接
// quiesce: 断开前的等待时间（毫秒）
func (s *clientEntity) Disconnect(quiesce uint) {
	s.mqttClient.Disconnect(quiesce)
}

// AddRoute 添加路由规则
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
