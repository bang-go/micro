package mqttx_test

import (
	"fmt"
	"github.com/bang-go/micro/mq/mqttx"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"log"
	"testing"
)

func TestClient(t *testing.T) {
	groupId := ""
	topic := ""
	deviceId := ""
	clientId := mqttx.GetClientId(groupId, deviceId)
	accessKeyId := ""
	accessKeySecret := ""
	instanceId := ""
	endpoint := ""
	username := mqttx.GetUserName(mqttx.AuthModeSignature, accessKeyId, instanceId)
	password := mqttx.GetSignPassword(clientId, accessKeySecret)
	log.Println(username)
	log.Println(password)
	client, err := mqttx.New(&mqttx.Config{AccessKeyId: accessKeyId, AccessKeySecret: accessKeySecret, Endpoint: endpoint, InstanceId: instanceId, GroupId: groupId, DeviceId: deviceId})
	if err != nil {
		log.Fatal(err)
	}
	log.Println("-----")
	client.Subscribe(topic, 1, func(client mqtt.Client, msg mqtt.Message) {
		fmt.Printf("subscribe message: %s from topic: %s\n", msg.Payload(), msg.Topic())
	})
	client.Publish(topic, 0, false, "123")
	client.Disconnect(1000)
}