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
	LoadingView
	ErrorView
	QrView
	ChatListView
	ChatView
	ProfileView
)

type Response uint8

const (
	ResponsePushView Response = iota
	ResponsePushViewNoWait
	ResponseReplaceView
	ResponseBackView
	ResponseIgnore
)

type UiMessage struct {
	Identifier Message
	Payload    interface{}
	Intent     Response
}

type UiView interface {
	gtk.Widgetter
	Update(msg *UiMessage) (Response, error)
	Done() <-chan struct{}
	Close()
	Title() string
}

type UiParent interface {
	QueueMessage(v Message, payload interface{})
	QueueMessageWithIntent(v Message, payload interface{}, intent Response)
	GetChatClient() *whatsmeow.Client
	GetChatDB() *db.Database
	GetContacts() (map[types.JID]types.ContactInfo, error)
	GetDeviceJID() *types.JID
	GetWindowSize() (int, int)
}
