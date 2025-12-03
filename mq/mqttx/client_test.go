package mqttx_test

import (
	"fmt"
	"log"
	"testing"

	"github.com/bang-go/micro/mq/mqttx"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func TestClient(t *testing.T) {
	groupId := ""
	topic := ""
	deviceId := ""
	//clientId := mqttx.GetClientId(groupId, deviceId)
	accessKeyId := ""
	accessKeySecret := ""
	instanceId := ""
	endpoint := ""
	//username := mqttx.GetUserName(mqttx.AuthModeSignature, accessKeyId, instanceId)
	//password := mqttx.GetSignPassword(clientId, accessKeySecret)
	client, err := mqttx.New(&mqttx.Config{AccessKeyId: accessKeyId, AccessKeySecret: accessKeySecret, Endpoint: endpoint, InstanceId: instanceId, GroupId: groupId, DeviceId: deviceId})
	if err != nil {
		log.Fatal(err)
	}
	log.Println("-----")
	_ = client.Subscribe(topic, 1, func(client mqtt.Client, msg mqtt.Message) {
		fmt.Printf("subscribe message: %s from topic: %s\n", msg.Payload(), msg.Topic())
	})
	_ = client.Publish(topic, 0, false, "foo")
	client.Disconnect(1000)
}
