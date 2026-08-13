package user

import (
	"GoAI/common/code"
	myemail "GoAI/common/email"
	myredis "GoAI/common/redis"
	"GoAI/dao/user"
	"GoAI/model"
	"GoAI/utils"
	"GoAI/utils/myjwt"
	"strings"
)

func Login(username, password string) (string, code.Code) {
	// [第一阶段优化-校验] 统一清理账号输入，降低空白字符造成的歧义。

	username = strings.TrimSpace(username)
	var userInformation *model.User
	var ok bool
	//1:判断用户是否存在

	if ok, userInformation = user.IsExistUser(username); !ok {
		return "", code.CodeInvalidPassword
	}
	// [安全优化] bcrypt 验证新密码，同时兼容尚未迁移的旧 MD5 密码。
	valid, needsUpgrade := utils.VerifyPassword(userInformation.Password, password)
	if !valid {
		return "", code.CodeInvalidPassword
	}
	if needsUpgrade {
		// [历史兼容] 旧密码验证成功后立即升级，后续登录不再使用 MD5。
		passwordHash, err := utils.HashPassword(password)
		if err != nil || user.UpdatePasswordHash(userInformation.ID, passwordHash) != nil {
			return "", code.CodeServerBusy
		}
	}
	//3:返回一个Token
	token, err := myjwt.GenerateToken(userInformation.ID, userInformation.Username)

	if err != nil {
		return "", code.CodeServerBusy
	}
	return token, code.CodeSuccess
}

func Register(email, password, captcha string) (string, code.Code) {
	// [第一阶段优化-校验] 邮箱标准化后再查重和入库。
	email = strings.ToLower(strings.TrimSpace(email))
	captcha = strings.TrimSpace(captcha)

	var ok bool
	var userInformation *model.User

	//1:先判断用户是否已经存在了
	if user.IsExistEmail(email) {
		return "", code.CodeUserExist
	}

	//2:从redis中验证验证码是否有效
	if ok, _ := myredis.CheckCaptchaForEmail(email, captcha); !ok {
		return "", code.CodeInvalidCaptcha
	}

	//3：生成11位的账号
	username := utils.GetRandomNumbers(11)

	//4：注册到数据库中
	if userInformation, ok = user.Register(username, email, password); !ok {
		return "", code.CodeServerBusy
	}

	//5：将账号一并发送到对应邮箱上去，后续需要账号登录
	if err := myemail.SendCaptcha(email, username, user.UserNameMsg); err != nil {
		return "", code.CodeServerBusy
	}

	// 6:生成Token
	token, err := myjwt.GenerateToken(userInformation.ID, userInformation.Username)

	if err != nil {
		return "", code.CodeServerBusy
	}

	return token, code.CodeSuccess
}

// 往指定邮箱发送验证码
// 分为以下任务：
// 1：先存放redis
// 2：再进行远程发送
func SendCaptcha(email_ string) code.Code {
	// [第一阶段优化-认证] 标准化邮箱并检查安全随机数生成失败。
	email_ = strings.ToLower(strings.TrimSpace(email_))
	send_code := utils.GetRandomNumbers(6)
	if send_code == "" {
		return code.CodeServerBusy
	}
	//1:先存放到redis
	if err := myredis.SetCaptchaForEmail(email_, send_code); err != nil {
		return code.CodeServerBusy
	}

	//2:再进行远程发送
	if err := myemail.SendCaptcha(email_, send_code, myemail.CodeMsg); err != nil {
		return code.CodeServerBusy
	}

	return code.CodeSuccess
}
