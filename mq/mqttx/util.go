package mqttx

import (
	"fmt"
	"github.com/bang-go/util/cipher"
)

func GetUserName(authMode, accessKeyId, instanceId string) string {
	return fmt.Sprintf("%s|%s|%s", authMode, accessKeyId, instanceId)
}

// GetSignPassword 获取签名模式下password
func GetSignPassword(clientId, accessKeySecret string) string {
	return cipher.HmacSha1(accessKeySecret, clientId)
}

func GetClientId(groupId, deviceId string) string {
	return fmt.Sprintf("%s@@@%s", groupId, deviceId)
}
