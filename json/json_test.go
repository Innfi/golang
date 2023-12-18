package test_json

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

type UserInfo struct {
	Email string
	Pass  string
}

func TestSomething(t *testing.T) {
	assert := assert.New(t)

	var a string = "hello"
	var b string = "hello"

	assert.Equal(a, b, "error message if false")
}

func TestJsonBehavior(t *testing.T) {
	assert := assert.New(t)

	userInfo := UserInfo{
		Email: "innfi@test.com",
		Pass:  "dummypass",
	}

	byteData, _ := json.Marshal(userInfo)

	result := UserInfo{}
	json.Unmarshal([]byte(byteData), &result)

	assert.Equal(userInfo.Email, result.Email)
	assert.Equal(userInfo.Pass, result.Pass)
}
