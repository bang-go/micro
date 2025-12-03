package mqtt_test

import (
	"fmt"
	"log"
	"testing"

	"github.com/bang-go/micro/mq/mqtt"
	pahomqtt "github.com/eclipse/paho.mqtt.golang"
)

func TestClient(t *testing.T) {
	groupId := ""
	topic := ""
	deviceId := ""
	//clientId := mqtt.GetClientId(groupId, deviceId)
	accessKeyId := ""
	accessKeySecret := ""
	instanceId := ""
	endpoint := ""
	//username := mqtt.GetUserName(mqtt.AuthModeSignature, accessKeyId, instanceId)
	//password := mqtt.GetSignPassword(clientId, accessKeySecret)
	client, err := mqtt.New(&mqtt.Config{AccessKeyId: accessKeyId, AccessKeySecret: accessKeySecret, Endpoint: endpoint, InstanceId: instanceId, GroupId: groupId, DeviceId: deviceId})
	if err != nil {
		log.Fatal(err)
	}
	log.Println("-----")
	_ = client.Subscribe(topic, 1, func(client pahomqtt.Client, msg pahomqtt.Message) {
		fmt.Printf("subscribe message: %s from topic: %s\n", msg.Payload(), msg.Topic())
	})
	_ = client.Publish(topic, 0, false, "foo")
	client.Disconnect(1000)
}
