package view

import (
	"context"
	"fmt"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	history "go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type chatItemRow struct {
	*gtk.ListBoxRow
	ui      *gtk.Box
	chat    *history.Conversation
	title   *gtk.Label
	profile *gtk.Image
}

func NewChatRow(chat *history.Conversation) (*chatItemRow, error) {
	chatItemRow := chatItemRow{
		ListBoxRow: gtk.NewListBoxRow(),
		chat:       chat,
	}
	ui := gtk.NewBox(gtk.OrientationHorizontal, 5)
	chatItemRow.ListBoxRow.SetChild(ui)

	profile := gtk.NewImageFromIconName("avatar-default-symbolic")
	profile.SetVExpand(true)
	ui.Append(profile)
	chatItemRow.profile = profile

	title := gtk.NewLabel(chat.GetDisplayName())
	title.SetVExpand(true)
	title.SetVAlign(gtk.AlignFill)
	ui.Append(title)

	chatItemRow.title = title
	chatItemRow.ui = ui

	err := chatItemRow.Update(chat)
	if err != nil {
		return nil, err
	}

	return &chatItemRow, nil
}

func (ci *chatItemRow) Update(chat *history.Conversation) error {
	ci.title.SetLabel(chat.GetDisplayName())
	ci.profile.ConnectShow(ci.UpdateProfileImage)

	return nil
}

func (ci *chatItemRow) UpdateProfileImage() {

}

type ChatUiView struct {
	*gtk.ScrolledWindow
	parent   UiParent
	view     *gtk.Box
	login    *gtk.Button
	chats    *gtk.ListBox
	contacts map[types.JID]types.ContactInfo

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

	v.chats = gtk.NewListBox()
	v.chats.SetHExpand(true)
	v.chats.SetVExpand(true)
	v.chats.SetVisible(false)
	v.view.Append(v.chats)

	viewport := gtk.NewViewport(nil, nil)
	viewport.SetScrollToFocus(true)
	viewport.SetChild(v.view)

	v.ScrolledWindow = gtk.NewScrolledWindow()
	v.ScrolledWindow.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	v.ScrolledWindow.SetChild(viewport)
	v.ScrolledWindow.SetPropagateNaturalHeight(true)

	return &v
}

func (ch *ChatUiView) chatEventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Connected:
		ch.login.SetVisible(false)
		ch.chats.SetVisible(true)
		client := ch.parent.GetChatClient()
		contacts, err := client.Store.Contacts.GetAllContacts()
		if err != nil {
			ch.parent.QueueMessage(ErrorView, err)
			return
		}
		ch.contacts = contacts

	case *events.Disconnected:
		ch.Close()
		ch.login.SetVisible(true)
		ch.chats.SetVisible(false)

	case *events.Message:
		fmt.Println(fmt.Println(v.Info.Chat.User, v.Message))

	case *events.HistorySync:
		ch.HistorySync(v)

	case *events.LoggedOut:
		ch.Close()
		ch.login.SetVisible(true)
		ch.chats.SetVisible(false)
	}
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

	if msg.Error != nil {
		return msg.Error
	}

	client := ch.parent.GetChatClient()
	if client == nil || !client.IsConnected() {
		ch.login.SetVisible(true)
		ch.chats.SetVisible(false)
		return nil
	}

	// Bind the event handler
	ch.evtHandle = client.AddEventHandler(ch.chatEventHandler)

	return nil
}

func (ch *ChatUiView) HistorySync(evt *events.HistorySync) error {
	fmt.Println("Got history sync!")

	for _, chat := range evt.Data.Conversations {
		chatItemRow, err := NewChatRow(chat)
		if err != nil {
			return err
		}

		ch.chats.Append(chatItemRow)
	}
	ch.login.SetVisible(false)
	ch.chats.SetVisible(true)

	return nil
}
