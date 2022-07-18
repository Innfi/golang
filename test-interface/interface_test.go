package test_interface

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type Interactor interface {
	ToEffectiveData() string
	SetEmail(input_email string)
	SetName(input_name string)
	ToEmail() string
}

type UserInfo struct {
	email string
	name  string
}

func (userInfo UserInfo) ToEffectiveData() string {
	return fmt.Sprintf("%s:%s", userInfo.name, userInfo.email)
}

func (userInfo *UserInfo) SetEmail(input_email string) {
	userInfo.email = input_email
	fmt.Println("email: ", userInfo.email)
}

func (userInfo *UserInfo) SetName(input_name string) {
	userInfo.name = input_name
}

func (userInfo UserInfo) ToEmail() string {
	return userInfo.email
}

func TestSomething(t *testing.T) {
	assert := assert.New(t)

	var interactor Interactor = &UserInfo{}
	interactor.SetEmail("innfi@test.com")
	interactor.SetName("tester")

	assert.Equal(interactor.ToEmail(), "innfi@test.com")
	assert.Equal(interactor.ToEffectiveData(), "tester:innfi@test.com")
}
