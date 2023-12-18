package main

import (
	"fmt"

	validator "github.com/go-playground/validator/v10"
)

type TestUser struct {
	Name  string `validate:"required"`
	Email string `validate:"min=10,max=40"`
}

var validate = validator.New()

func main() {
	user := TestUser{Name: "innfi", Email: "longemailforvalidation@test.com"}

	errs := validate.Struct(user)
	if errs == nil {
		fmt.Println("input is valid")
		return
	}

	for _, err := range errs.(validator.ValidationErrors) {
		fmt.Println("field: ", err.Field())
		fmt.Println("field: ", err.Tag())
		fmt.Println("value: ", err.Value())
	}
}
