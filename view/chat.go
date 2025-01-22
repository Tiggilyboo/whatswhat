package view

import (
	"context"
	"fmt"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

type ChatUiView struct {
	*gtk.ScrolledWindow
	parent UiParent
	view   *gtk.Box
	login  *gtk.Button

	ctx       context.Context
	cancel    context.CancelFunc
	evtHandle uint32
}

func NewChatView(parent UiParent) *ChatUiView {
	ctx, cancel := context.WithCancel(context.Background())

	v := ChatUiView{
		ctx:    ctx,
		cancel: cancel,
		parent: parent,
	}
	context.AfterFunc(ctx, v.Close)

	v.view = gtk.NewBox(gtk.OrientationVertical, 0)

	v.login = gtk.NewButtonWithLabel("Login")
	v.login.ConnectClicked(func() {
		parent.QueueMessage(QrView, nil)
	})
	v.view.Append(v.login)

	viewport := gtk.NewViewport(nil, nil)
	viewport.SetScrollToFocus(true)
	viewport.SetChild(v.view)

	v.ScrolledWindow = gtk.NewScrolledWindow()
	v.ScrolledWindow.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	v.ScrolledWindow.SetChild(viewport)
	v.ScrolledWindow.SetPropagateNaturalHeight(true)

	return &v
}

func (ch *ChatUiView) Done() <-chan struct{} {
	return ch.ctx.Done()
}

func (ch *ChatUiView) Close() {
	chat := ch.parent.GetChatClient()
	if ch.evtHandle != 0 && chat != nil {
		chat.RemoveEventHandler(ch.evtHandle)
	}

	if ch.ctx.Done() != nil {
		ch.cancel()
	}
}

func (ch *ChatUiView) Title() string {
	return "WhatsWhat - Chat"
}

func (ch *ChatUiView) Update(msg *UiMessage) error {
	fmt.Println("ViewChats: Invoked")

	client := ch.parent.GetChatClient()
	if client == nil || !client.IsLoggedIn() {
		ch.login.SetVisible(true)
		return nil
	}

	// Client is logged in!
	fmt.Println("Logged in")
	ch.login.SetVisible(false)

	contacts, err := client.Store.Contacts.GetAllContacts()
	for jid, contact := range contacts {

	}
	// Bind the event handler
	ch.evtHandle = client.AddEventHandler(ch.chatEventHandler)

	return nil
}

func (ch *ChatUiView) bindEventHandler() {
}

func (ch *ChatUiView) chatEventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		fmt.Println(fmt.Println(v.Info.Chat.User, v.Message))

	case *events.Disconnected:
		ch.Update(&UiMessage{
			Identifier: ErrorView,
			Payload:    "Disconnected",
		})
	}
}
