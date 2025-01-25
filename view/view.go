package view

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"go.mau.fi/whatsmeow"
)

type Message uint8

const (
	Undefined Message = iota
	Close
	LoadingView
	ErrorView
	QrView
	ChatView
	ProfileView
)

type UiMessage struct {
	Identifier Message
	Payload    interface{}
	Error      error
}

type UiView interface {
	gtk.Widgetter
	Update(msg *UiMessage) error
	Done() <-chan struct{}
	Close()
	Title() string
}

type UiParent interface {
	QueueMessage(v Message, payload interface{})
	GetChatClient() *whatsmeow.Client
}
