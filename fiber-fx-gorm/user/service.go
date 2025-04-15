package user

import (
	"log"
)

type UserService struct {
	repo *UserRepo
}

func (service UserService) CreateUser(payload UserPayload) (*User, error) {
	err := service.repo.Save(payload)
	if err != nil {
		log.Println("failed to create user")
		return nil, err
	}

	return service.repo.FindOne(payload.Email)
}

func (service UserService) FindUser(email string) (*User, error) {
	user, err := service.repo.FindOne(email)
	if err != nil {
		log.Println("user not found: ", email)
		return nil, err
	}

	return user, nil
}

func InitUserService(repo *UserRepo) *UserService {
	log.Println("InitUserService] ")
	return &UserService{
		repo: repo,
	}
}
