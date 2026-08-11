package user

import (
	"GopherAI/common/mysql"
	"GopherAI/model"
	"GopherAI/utils"
	"context"

	"gorm.io/gorm"
)

const (
	CodeMsg     = "GopherAI验证码如下(验证码仅限于2分钟有效): "
	UserNameMsg = "GopherAI的账号如下，请保留好，后续可以用账号进行登录 "
)

var ctx = context.Background()

// 这边只能通过账号进行登录
func IsExistUser(username string) (bool, *model.User) {

	user, err := mysql.GetUserByUsername(username)

	if err == gorm.ErrRecordNotFound || user == nil {
		return false, nil
	}

	return true, user
}

// [第一阶段优化-认证] 注册前按邮箱检查重复账号。
func IsExistEmail(email string) bool {
	var count int64
	return mysql.DB.Model(&model.User{}).Where("email = ?", email).Count(&count).Error == nil && count > 0
}

func Register(username, email, password string) (*model.User, bool) {
	// [安全优化] 注册时只保存 bcrypt 哈希，不保存明文或 MD5。
	passwordHash, err := utils.HashPassword(password)
	if err != nil {
		return nil, false
	}
	if user, err := mysql.InsertUser(&model.User{
		Email:    email,
		Name:     username,
		Username: username,
		Password: passwordHash,
	}); err != nil {
		return nil, false
	} else {
		return user, true
	}
}

func UpdatePasswordHash(userID int64, passwordHash string) error {
	// [历史兼容] 旧用户首次成功登录后，用 bcrypt 哈希覆盖旧 MD5。
	return mysql.DB.Model(&model.User{}).Where("id = ?", userID).Update("password", passwordHash).Error
}
