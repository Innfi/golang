package test_interface

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type Interactor interface {
	toEffectiveData() string
	setEmail(input_email string)
	setName(input_name string)
}

type UserInfo struct {
	email string
	name  string
}

func (userInfo UserInfo) toEffectiveData() string {
	return fmt.Sprintf("%s:%s", userInfo.name, userInfo.email)
}

func (userInfo UserInfo) setEmail(input_email string) {
	userInfo.email = input_email
}

func (userInfo UserInfo) setName(input_name string) {
	userInfo.name = input_name
}

func TestSomething(t *testing.T) {
	assert := assert.New(t)

	var interactor Interactor = UserInfo{
		email: "innfi@test.com",
		name:  "tester",
	}

	assert.Equal(interactor.toEffectiveData(), "tester:innfi@test.com")
}
