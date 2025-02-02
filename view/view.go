package view

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/tiggilyboo/whatswhat/db"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type Message uint8

const (
	Undefined Message = iota
	Close
	LoadingView
	ErrorView
	QrView
	ChatListView
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
	GetChatDB() *db.Database
	GetContacts() (map[types.JID]types.ContactInfo, error)
}
