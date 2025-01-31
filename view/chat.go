package view

import (
	"context"
	"fmt"
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/tiggilyboo/whatswhat/db"
	"github.com/tiggilyboo/whatswhat/view/models"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type chatItemRow struct {
	*gtk.ListBoxRow
	parent UiParent
	chat   *models.ConversationInfo
	ui     *gtk.Box
	title  *gtk.Label
	status *gtk.Image
}

func NewChatRow(parent UiParent, chat *db.Conversation) (*chatItemRow, error) {
	chatItemRow := chatItemRow{
		ListBoxRow: gtk.NewListBoxRow(),
		parent:     parent,
	}
	ui := gtk.NewBox(gtk.OrientationHorizontal, 5)
	chatItemRow.ListBoxRow.SetChild(ui)

	chatInfo, err := models.GetConversationInfo(parent.GetChatClient(), chat.ChatJID)
	if err != nil {
		return nil, err
	}

	title := gtk.NewLabel(chatInfo.Name)
	title.SetVExpand(true)
	title.SetVAlign(gtk.AlignFill)
	ui.Append(title)

	status := gtk.NewImageFromIconName("media-record-symbolic")
	status.SetVExpand(true)
	status.SetHAlign(gtk.AlignEnd)
	ui.Append(status)

	chatItemRow.title = title
	chatItemRow.ui = ui

	if err := chatItemRow.Update(chatInfo); err != nil {
		return nil, err
	}

	return &chatItemRow, nil
}

func (ci *chatItemRow) Update(model *models.ConversationInfo) error {
	ci.title.SetLabel(model.Name)
	ci.chat = model

	return nil
}

type ChatUiView struct {
	*gtk.ScrolledWindow
	parent   UiParent
	view     *gtk.Box
	login    *gtk.Button
	chats    *gtk.ListBox
	contacts map[types.JID]types.ContactInfo
	synced   bool
	syncing  bool

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

	switch evt.(type) {
	case *events.Connected:
		fmt.Println("chatEventHandler: Connected")
		ch.login.SetVisible(false)
		ch.chats.SetVisible(true)
		client := ch.parent.GetChatClient()
		contacts, err := client.Store.Contacts.GetAllContacts()
		if err != nil {
			ch.parent.QueueMessage(ErrorView, err)
			return
		}
		fmt.Println("loaded ", len(contacts), " contacts")
		ch.contacts = contacts

	case *events.Disconnected:
		fmt.Println("chatEventHandler: Disconnected")
		ch.Close()
		ch.login.SetVisible(true)
		ch.chats.SetVisible(false)

	case *events.LoggedOut:
		fmt.Println("chatEventHandler: Logged out")
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
	if ch.evtHandle != 0 {
		ch.Close()
	}
	ch.ctx, ch.cancel = context.WithDeadline(context.Background(), time.Now().Add(2*time.Second))
	defer ch.cancel()

	// Bind the event handler
	ch.evtHandle = client.AddEventHandler(ch.chatEventHandler)

	fmt.Println("Upgrading chat database")
	chatDb := ch.parent.GetChatDB()
	go chatDb.Upgrade(ch.ctx)

	fmt.Println("Getting conversations...")
	conversations, err := chatDb.Conversation.GetRecent(ch.ctx, *client.Store.ID, 50)
	if err != nil {
		return err
	}

	fmt.Printf("Got %s conversations\n", len(conversations))
	for _, convo := range conversations {
		chatRow, err := NewChatRow(ch.parent, convo)
		if err != nil {
			return err
		}

		fmt.Printf("Appending %s\n", chatRow.chat.Name)
		ch.chats.Append(chatRow)
	}

	fmt.Println("ChatUiView: Done")
	return nil
}
