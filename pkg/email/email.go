package email

import (
	"crypto/tls"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"gopkg.in/gomail.v2"
)

type Email struct {
	*gomail.Dialer
	log *zap.Logger
}

func New(log *zap.Logger) *Email {
	e := Email{
		gomail.NewDialer(
			viper.GetString("email.host"),
			viper.GetInt("email.port"),
			viper.GetString("email.username"),
			viper.GetString("email.password"),
		),
		log,
	}
	e.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	return &e
}
func (e *Email) Send(addr []string, content string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", e.Username)
	m.SetHeader("To", addr...)
	m.SetBody("text/html", content)
	if err := e.DialAndSend(m); err != nil {
		e.log.Error("邮件发送失败", zap.Error(err))
		return err
	}
	return nil
}
