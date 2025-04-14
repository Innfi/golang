package user

import (
	common "fiber-fx-gorm/common"

	"gorm.io/gorm"
)

type UserRepository interface {
	Save(payload UserPayload) (*User, error)
	FindOne(email string) (*User, error)
}

type UserRepo struct {
	db *gorm.DB
}

func (repo UserRepo) Save(payload UserPayload) (*User, error) {
	return nil, nil
}

func (repo UserRepo) FindOne(email string) (*User, error) {
	return nil, nil
}

func (repo UserRepo) FindOneWithJoin(email string) (*JoinedUser, error) {
	var joinedUser JoinedUser

	repo.db.Model(&User{}).Select("users.id, users.email, emailStat.suppressedFor, emailStat.hardbouncedFor").Joins("left join emailStats on users.email=emailStat.email").Scan(&joinedUser)

	return &joinedUser, nil
}

func InitUserRepo(handle *common.DatabaseHandle) *UserRepo {
	return &UserRepo{
		db: handle.Db,
	}
}
