package view

import (
	"context"
	"fmt"
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
	"github.com/tiggilyboo/whatswhat/db"
	"github.com/tiggilyboo/whatswhat/view/models"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type chatItemRow struct {
	*gtk.ListBoxRow
	parent               UiParent
	chat                 *models.ConversationModel
	ui                   *gtk.Box
	uiTop                *gtk.Box
	uiBottom             *gtk.Box
	title                *gtk.Label
	lastMessage          *gtk.Label
	lastMessageTimestamp *gtk.Label
	status               *gtk.Image
}

func NewChatRow(ctx context.Context, parent UiParent, chat *db.Conversation) (*chatItemRow, error) {
	//fmt.Println("NewChatRow")
	client := parent.GetChatClient()
	contacts, err := parent.GetContacts()
	if err != nil {
		return nil, err
	}
	pushNames, err := parent.GetPushNames()
	if err != nil {
		return nil, err
	}
	unread := uint(0)
	if chat.UnreadCount != nil {
		unread = uint(*chat.UnreadCount)
	}
	chatInfo, err := models.GetConversationModel(client, contacts, pushNames, chat.ChatJID, chat.Name, unread, chat.LastMessageTimestamp, false)
	if err != nil {
		return nil, fmt.Errorf("Error GetConversationModel: %s", err.Error())
	}

	chatItemRow := chatItemRow{
		parent: parent,
	}

	ui := gtk.NewBox(gtk.OrientationVertical, 5)
	uiTop := gtk.NewBox(gtk.OrientationHorizontal, 5)
	uiBottom := gtk.NewBox(gtk.OrientationHorizontal, 5)
	ui.Append(uiTop)
	ui.Append(uiBottom)

	chatItemRow.ui = ui
	chatItemRow.uiTop = uiTop
	chatItemRow.uiBottom = uiBottom

	chatItemRow.ListBoxRow = gtk.NewListBoxRow()
	chatItemRow.ListBoxRow.SetChild(ui)

	if err := chatItemRow.Update(ctx, chatInfo); err != nil {
		return nil, fmt.Errorf("Error chatItemRow.Update: %s", err.Error())
	}

	return &chatItemRow, nil
}

func (ci *chatItemRow) Update(ctx context.Context, model *models.ConversationModel) error {
	ci.chat = model

	titleText := model.Name
	if len(titleText) == 0 {
		titleText = model.ChatJID.User
	}
	title := gtk.NewLabel(titleText)
	titleFont := title.PangoContext().FontDescription()
	titleFont.SetSize(14 * pango.SCALE)
	title.PangoContext().SetFontDescription(titleFont)
	title.SetVExpand(true)
	title.SetVAlign(gtk.AlignFill)
	title.SetWrap(true)
	title.SetWrapMode(pango.WrapWordChar)
	if ci.title != nil {
		ci.uiTop.Remove(ci.title)
	}
	ci.title = title
	ci.uiTop.Append(title)

	var status *gtk.Image
	if model.UnreadCount > 0 {
		status = gtk.NewImageFromIconName("media-record-symbolic")
	} else {
		status = gtk.NewImage()
	}
	status.SetVExpand(true)
	status.SetHAlign(gtk.AlignStart)
	if ci.status != nil {
		ci.uiTop.Remove(ci.status)
	}
	ci.status = status
	ci.uiTop.Prepend(status)

	chatDb := ci.parent.GetChatDB()
	client := ci.parent.GetChatClient()
	deviceJID := client.Store.ID
	pushNames, err := ci.parent.GetPushNames()
	if err != nil {
		pushNames = make(map[types.JID]*db.PushName, 0)
	}

	mostRecentMessages, err := chatDb.Message.GetBetween(ctx, *deviceJID, model.ChatJID, nil, nil, 1)
	if err != nil {
		fmt.Printf("Unable to load last message for: %s", model.ChatJID)
		mostRecentMessages = make([]*db.Message, 0)
	}

	var lastMessageText *string
	var lastMessageTimestamp *time.Time
	if len(mostRecentMessages) > 0 {
		pushName := mostRecentMessages[0].SenderJID.User
		if dbPushName, ok := pushNames[mostRecentMessages[0].SenderJID.ToNonAD()]; ok {
			pushName = dbPushName.Name
		}
		messageModel, err := models.GetMessageModel(client, pushName, mostRecentMessages[0])
		if err == nil {
			lastMessageTrimmed := fmt.Sprintf("%.*s", 35, messageModel.Message)
			lastMessageText = &lastMessageTrimmed
			lastMessageTimestamp = &messageModel.Timestamp
		}
	}

	if ci.lastMessage != nil {
		ci.uiBottom.Remove(ci.lastMessage)
	}
	if lastMessageText != nil {
		lastMessage := gtk.NewLabel(*lastMessageText)
		lastMessage.SetHAlign(gtk.AlignStart)
		lastMessage.SetHExpand(true)
		ci.uiBottom.Append(lastMessage)
	}

	if ci.lastMessageTimestamp != nil {
		ci.uiBottom.Remove(ci.lastMessageTimestamp)
	}
	if lastMessageTimestamp != nil && !lastMessageTimestamp.IsZero() {
		lastMessageTimeLabel := gtk.NewLabel(lastMessageTimestamp.Local().Format(time.DateTime))
		lastMessageTimeLabel.SetHExpand(true)
		lastMessageTimeLabel.SetHAlign(gtk.AlignEnd)
		ci.uiBottom.Append(lastMessageTimeLabel)
	}

	return nil
}

type ChatListUiView struct {
	*gtk.ScrolledWindow
	parent   UiParent
	view     *gtk.Box
	login    *gtk.Button
	chatList *gtk.ListBox
	chats    []*chatItemRow
	contacts map[types.JID]types.ContactInfo

	ctx       context.Context
	cancel    context.CancelFunc
	evtHandle uint32
}

func NewChatListView(parent UiParent) *ChatListUiView {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	v := ChatListUiView{
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

	v.chatList = gtk.NewListBox()
	v.chatList.SetHExpand(true)
	v.chatList.SetVExpand(true)
	v.chatList.SetVisible(false)
	v.chatList.SetSelectionMode(gtk.SelectionSingle)
	v.chatList.UnselectAll()
	v.chatList.ConnectRowSelected(v.handleChatSelected)
	v.view.Append(v.chatList)

	viewport := gtk.NewViewport(nil, nil)
	viewport.SetScrollToFocus(true)
	viewport.SetChild(v.view)

	v.ScrolledWindow = gtk.NewScrolledWindow()
	v.ScrolledWindow.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	v.ScrolledWindow.SetChild(viewport)
	v.ScrolledWindow.SetPropagateNaturalHeight(true)

	return &v
}

func (ch *ChatListUiView) chatEventHandler(evt interface{}) {

	switch evt.(type) {
	case *events.Connected:
		fmt.Println("chatEventHandler: Connected")
		ch.login.SetVisible(false)
		ch.chatList.SetVisible(false)
		ch.parent.QueueMessage(ChatListView, nil)

	case *events.Disconnected:
		fmt.Println("chatEventHandler: Disconnected")
		ch.Close()
		ch.login.SetVisible(true)
		ch.chatList.SetVisible(false)

	case *events.LoggedOut:
		fmt.Println("chatEventHandler: Logged out")
		ch.Close()
		ch.login.SetVisible(true)
		ch.chatList.SetVisible(false)
	}
}

func (ch *ChatListUiView) Done() <-chan struct{} {
	return ch.ctx.Done()
}

func (ch *ChatListUiView) Close() {
	chat := ch.parent.GetChatClient()
	if ch.evtHandle != 0 && chat != nil {
		chat.RemoveEventHandler(ch.evtHandle)
	}
	if ch.cancel != nil {
		ch.cancel()
	}
	ch.chatList.SetVisible(false)
	ch.login.SetVisible(true)
}

func (ch *ChatListUiView) Title() string {
	return "Chats"
}

func (ch *ChatListUiView) handleChatSelected(row *gtk.ListBoxRow) {
	if row == nil {
		return
	}
	if !row.IsSelected() {
		return
	}
	fmt.Printf("Selected chat row: %s\n", row.Index())

	if row.Index() >= len(ch.chats) || row.Index() < 0 {
		ch.parent.QueueMessage(ErrorView, fmt.Errorf("Invalid chat row index: %d", row.Index()))
		return
	}
	chatRowUi := ch.chats[row.Index()]
	if chatRowUi == nil {
		ch.parent.QueueMessage(ErrorView, fmt.Errorf("Invalid chat row UI: %d", row.Index()))
		return
	}

	chat := chatRowUi.chat
	ch.chatList.UnselectAll()
	ch.parent.QueueMessage(ChatView, chat)
}

func (ch *ChatListUiView) Update(msg *UiMessage) (Response, error) {
	fmt.Println("ChatListUiView.Update: Invoked")

	// Clear any old state
	initialize := false
	if msg.Intent == ResponsePushView {
		ch.contacts = nil
		ch.chats = nil
		ch.chatList.RemoveAll()
		initialize = true
	}

	client := ch.parent.GetChatClient()
	if client == nil || !client.IsConnected() {
		ch.login.SetVisible(true)
		ch.chatList.SetVisible(false)
		return msg.Intent, nil
	}

	if initialize {
		if ch.cancel != nil {
			ch.cancel()
		}
		if ch.evtHandle != 0 {
			ch.Close()
		}
	}
	ch.ctx, ch.cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer ch.cancel()

	if initialize || ch.evtHandle == 0 {
		// Bind the event handler
		ch.evtHandle = client.AddEventHandler(ch.chatEventHandler)
	}

	if !client.IsLoggedIn() {
		ch.login.SetVisible(true)
		ch.chatList.SetVisible(false)
		return msg.Intent, nil
	}

	fmt.Println("Getting conversations from chat DB...")
	chatDb := ch.parent.GetChatDB()
	archived := false
	conversations, err := chatDb.Conversation.GetRecent(ch.ctx, *ch.parent.GetDeviceJID(), 100, archived)
	if err != nil {
		return ResponsePushView, fmt.Errorf("Error getting recent conversations: %s", err.Error())
	}

	fmt.Printf("Got %s conversations\n", len(conversations))

	ch.chatList.UnselectAll()
	ch.chatList.RemoveAll()

	chats := make([]*chatItemRow, len(conversations))
	for i, convo := range conversations {
		chatRow, err := NewChatRow(ch.ctx, ch.parent, convo)
		if err != nil {
			return ResponsePushView, fmt.Errorf("Error creating chat row: %s", err.Error())
		}

		// By default don't process any selection until a user clicks the item
		chatRow.SetSelectable(false)

		//fmt.Printf("Appending %s into row index: %d\n", chatRow.chat.Name, i)
		ch.chatList.Append(chatRow)
		chats[i] = chatRow
	}
	ch.chats = chats

	// Allow any selections
	ch.chatList.UnselectAll()
	for _, chatRow := range chats {
		chatRow.SetSelectable(true)
	}

	ch.chatList.SetVisible(true)
	ch.login.SetVisible(false)

	fmt.Println("ChatListUiView: Done")
	return msg.Intent, nil
}
