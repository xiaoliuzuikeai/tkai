package email

import (
	"GopherAI/config"
	"fmt"
	"strings"

	"gopkg.in/gomail.v2"
)

const (
	CodeMsg     = "GopherAI verification code, valid for 2 minutes:"
	UserNameMsg = "Your GopherAI username is:"
)

func SendCaptcha(email, code, msg string) error {
	emailConfig := config.GetConfig().EmailConfig
	if isPlaceholderEmailConfig(emailConfig.Email, emailConfig.Authcode) {
		fmt.Printf("[dev email] SMTP is not configured, skip sending to %s. Content: %s %s\n", email, msg, code)
		return nil
	}

	m := gomail.NewMessage()
	m.SetHeader("From", emailConfig.Email)
	m.SetHeader("To", email)
	m.SetHeader("Subject", "GopherAI message")
	m.SetBody("text/plain", msg+" "+code)

	d := gomail.NewDialer("smtp.qq.com", 587, emailConfig.Email, emailConfig.Authcode)
	if err := d.DialAndSend(m); err != nil {
		fmt.Printf("DialAndSend err %v:\n", err)
		return err
	}
	fmt.Printf("send mail success\n")
	return nil
}

func isPlaceholderEmailConfig(email, authcode string) bool {
	email = strings.TrimSpace(email)
	authcode = strings.TrimSpace(authcode)
	return email == "" ||
		authcode == "" ||
		email == "your qq email" ||
		authcode == "your authcode"
}
