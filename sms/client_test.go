package sms

import (
	"github.com/bang-go/util"
	"log"
	"testing"
)

func TestClient(t *testing.T) {
	client, err := New(&Config{
		AccessKeyId:     util.VarAddr(""),
		AccessKeySecret: util.VarAddr("")})
	if err != nil {
		log.Fatal(err)
	}
	resp, err := client.SendSms(&SendSmsRequest{
		TemplateCode:  util.VarAddr(""),
		TemplateParam: util.VarAddr(""),
		SignName:      util.VarAddr(""),
		PhoneNumbers:  util.VarAddr("")})
	if err != nil {
		log.Fatal(err)
	}
	log.Println(resp.String())
}
