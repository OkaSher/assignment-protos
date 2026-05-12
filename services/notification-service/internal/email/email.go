package email

import (
	"errors"
	"log"
	"math/rand"
	"net/smtp"
	"time"
)

type EmailSender interface {
	Send(to, subject, body, id string) error
}

// SMTP adapter
type SMTPAdapter struct {
	addr string
	user string
	pass string
}

func NewSMTPAdapter(addr, user, pass string) *SMTPAdapter {
	return &SMTPAdapter{addr: addr, user: user, pass: pass}
}

func (s *SMTPAdapter) Send(to, subject, body, id string) error {
	// simplified SMTP send
	auth := smtp.PlainAuth("", s.user, s.pass, s.addr)
	msg := []byte("To: " + to + "\r\nSubject: " + subject + "\r\n\r\n" + body)
	return smtp.SendMail(s.addr, auth, s.user, []string{to}, msg)
}

// Simulated adapter
type SimulatedAdapter struct{}

func NewSimulatedAdapter() *SimulatedAdapter { return &SimulatedAdapter{} }

func (s *SimulatedAdapter) Send(to, subject, body, id string) error {
	// simulate latency and random failure
	d := time.Duration(200+rand.Intn(800)) * time.Millisecond
	time.Sleep(d)
	if rand.Intn(10) < 2 { // 20% failure
		log.Printf("[Simulated] failed sending email to %s (id=%s)", to, id)
		return errors.New("simulated provider error")
	}
	log.Printf("[Simulated] sent email to %s (id=%s)", to, id)
	return nil
}
