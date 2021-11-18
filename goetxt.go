package goetxt

import (
	"crypto/tls"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/malaow3/trunk"
	"github.com/sirupsen/logrus"
	"gopkg.in/gomail.v2"
)

const gmailPort = 587
const phoneLen = 10
const timeSleep = 5

type Client struct {
	// Must be gmail account
	username   string
	password   string
	extMap     map[MessageType]string
	imapclient *client.Client
	smtpclient *gomail.Dialer
}

type Message struct {
	MessageType MessageType
	Message     string
	// 10 digit phone number, without dashes
	Recipient string
	Time      time.Time
}

type MessageType int

const (
	ATTSMS         MessageType = 0x01
	ATTMMS         MessageType = 0x02
	BoostSMS       MessageType = 0x03
	BoostMMS       MessageType = 0x04
	CricketSMS     MessageType = 0x05
	CricketMMS     MessageType = 0x06
	SprintSMS      MessageType = 0x07
	SprintMMS      MessageType = 0x08
	StraighTalkSMS MessageType = 0x09
	StraighTalkMMS MessageType = 0x0A
	TMobileSMS     MessageType = 0x0B
	TMobileMMS     MessageType = 0x0C
	USCellularSMS  MessageType = 0x0D
	USCellularMMS  MessageType = 0x0E
	VerizonSMS     MessageType = 0x0F
	VerizonMMS     MessageType = 0x10
	VirginSMS      MessageType = 0x11
	VirginMMS      MessageType = 0x12
)

func (inst *Client) Init(username, password string) {
	trunk.InitLogger()

	if !strings.HasSuffix(username, "@gmail.com") {
		logrus.Fatal("Username must be gmail account")
	}
	inst.username = username
	inst.password = password
	inst.extMap = map[MessageType]string{
		ATTSMS:         "@txt.att.net",
		ATTMMS:         "@mms.att.net",
		BoostSMS:       "@sms.myboostmobile.com",
		BoostMMS:       "@myboostmobile.com",
		CricketSMS:     "@mms.cricketwireless.net",
		CricketMMS:     "@mms.cricketwireless.net",
		SprintSMS:      "@messaging.sprintpcs.com",
		SprintMMS:      "@pm.sprint.com",
		StraighTalkSMS: "@vtext.com",
		StraighTalkMMS: "@mypixmessages.com",
		TMobileSMS:     "@tmomail.net",
		TMobileMMS:     "@tmomail.net",
		USCellularSMS:  "@email.uscc.net",
		USCellularMMS:  "@mms.uscc.net",
		VerizonSMS:     "@vtext.com",
		VerizonMMS:     "@vzwpix.com",
		VirginSMS:      "@vmobl.com",
		VirginMMS:      "@vmpix.com",
	}

	// Connect to server
	impaclient, err := client.DialTLS("imap.gmail.com:993", nil)
	if err != nil {
		logrus.Panic(err)
	}

	// Login
	if err = impaclient.Login(inst.username, inst.password); err != nil {
		logrus.Panic(err)
	}

	inst.imapclient = impaclient

	// Settings for SMTP server
	smtpclient := gomail.NewDialer("smtp.gmail.com", gmailPort, inst.username, inst.password)
	// This is only needed when SSL/TLS certificate is not valid on server.
	// In production this should be set to false.
	smtpclient.TLSConfig = &tls.Config{
		InsecureSkipVerify: false,
		MinVersion:         tls.VersionTLS12,
		ServerName:         "smtp.gmail.com",
	}
	inst.smtpclient = smtpclient
}

func Init(username, password string) *Client {
	inst := &Client{}
	inst.Init(username, password)
	return inst
}

func (inst *Client) SendSMS(msg Message) error {
	trunk.InitLogger()
	if len(msg.Recipient) != phoneLen {
		logrus.Error("Recipient must be 10 digit phone number")
		return errors.New("Recipient must be 10 digit phone number")
	}

	m := gomail.NewMessage()
	m.SetHeader("From", inst.username)
	m.SetHeader("To", msg.Recipient+inst.extMap[msg.MessageType])

	// Set E-Mail body. You can set plain text or html with text/html
	m.SetBody("text/plain", msg.Message)

	// Now send E-Mail
	if err := inst.smtpclient.DialAndSend(m); err != nil {
		logrus.Error(err)
		logrus.Info("Please make sure that 'Less secure app access' is enabled in your Google account settings")
		return err
	}
	return nil
}

func (inst *Client) GetInboxes() ([]*imap.MailboxInfo, error) {
	// Connect to server

	mailboxesCh := make(chan *imap.MailboxInfo, 1)
	done := make(chan error, 1)
	go func() {
		done <- inst.imapclient.List("", "*", mailboxesCh)
	}()

	mailboxes := []*imap.MailboxInfo{}

	for item := <-mailboxesCh; item != nil; {
		mailboxes = append(mailboxes, item)
		item = <-mailboxesCh
	}
	if err := <-done; err != nil {
		return nil, err
	}

	return mailboxes, nil
}

// GetMessages returns all messages in a specified inbox
// If recipient is not empty, only messages with that recipient will be returned
// If number is not empty, only 1 message will be returned.
func (inst *Client) GetMessages(mailbox string, recipient *string, number *uint32) ([]*Message, error) {

	if number != nil && *number == 0 {
		return nil, errors.New("Cannot fetch 0 messages")
	}

	// Select INBOX
	mbox, err := inst.imapclient.Select(mailbox, false)
	if err != nil {
		return nil, err
	}

	from := uint32(1)
	to := mbox.Messages
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, to)

	messageCh := make(chan *imap.Message, 1)
	done := make(chan error, 1)

	go func() {
		done <- inst.imapclient.Fetch(seqset, []imap.FetchItem{imap.FetchItem("BODY.PEEK[]"), imap.FetchEnvelope}, messageCh)
	}()

	messages := []*imap.Message{}

	for item := <-messageCh; item != nil; {
		fromAdd := item.Envelope.From[0].Address()
		if recipient != nil && fromAdd[0:10] != *recipient {
			item = <-messageCh
			continue
		}

		if _, convertErr := strconv.Atoi(fromAdd[0:10]); convertErr != nil {
			item = <-messageCh
			continue
		}
		messages = append(messages, item)
		item = <-messageCh
	}

	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	msgs := []*Message{}

	msgLen := uint32(len(messages))
	if number != nil && msgLen > *number {
		msgLen = *number
	}

	for _, msg := range messages[0:msgLen] {
		data := msg.Body
		for _, literal := range data {
			buffer := make([]byte, literal.Len())
			_, readErr := literal.Read(buffer)
			if readErr != nil {
				logrus.Error(readErr)
				return nil, readErr
			}
			reg := regexp.MustCompile(`Content-Location: text_0.txt(?P<TEXT>(.|\n)*)--__CONTENT`)
			msgText := strings.TrimSpace(reg.FindStringSubmatch(string(buffer))[1])
			msgs = append(msgs, &Message{
				Message:   msgText,
				Recipient: msg.Envelope.From[0].Address()[0:10],
				Time:      msg.Envelope.Date,
			})

		}
	}

	if err = <-done; err != nil {
		return nil, err
	}

	return msgs, nil
}

func (inst *Client) OnMessage(handler func(msg *Message)) {
	go func() {
		for {
			msgcount := uint32(1)
			messages, err := inst.GetMessages("INBOX", nil, &msgcount)
			if err != nil {
				logrus.Error(err)
				return
			}
			lastmsg := messages[0]
			for lastmsg.equal(messages[0]) {
				time.Sleep(time.Second * timeSleep)
				messages, err = inst.GetMessages("INBOX", nil, &msgcount)
				if err != nil {
					logrus.Error(err)
					return
				}
			}
			go handler(messages[len(messages)-1])
		}
	}()
}

func (inst *Message) equal(msg *Message) bool {
	return inst.Recipient == msg.Recipient && inst.Message == msg.Message && inst.Time.Unix() == msg.Time.Unix()
}

func (inst *Client) KeepAlive() {
	keepAlive := make(chan int)
	<-keepAlive
}
