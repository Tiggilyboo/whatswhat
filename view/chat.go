package view

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/tiggilyboo/whatswhat/view/models"
	"go.mau.fi/whatsmeow/types/events"
)

type messageRowUi struct {
	*gtk.ListBoxRow
	parent    UiParent
	message   *models.MessageModel
	ui        *gtk.Box
	uiTop     *gtk.Box
	uiBottom  *gtk.Box
	timestamp *gtk.Label
	text      *gtk.Label
	loaded    bool
}

func (mr *messageRowUi) Timestamp() *time.Time {
	return &mr.message.Timestamp
}

func NewMessageRowUi(ctx context.Context, parent UiParent, message *models.MessageModel) *messageRowUi {
	ui := gtk.NewBox(gtk.OrientationVertical, 0)
	uiTop := gtk.NewBox(gtk.OrientationHorizontal, 5)
	uiBottom := gtk.NewBox(gtk.OrientationHorizontal, 5)

	ui.SetMarginTop(15)
	ui.SetHExpand(true)
	ui.SetVExpand(true)
	ui.Append(uiTop)
	ui.Append(uiBottom)

	text := gtk.NewLabel("...")
	text.SetHExpand(true)
	text.SetHAlign(gtk.AlignStart)
	uiTop.Append(text)

	var timestampLabel *gtk.Label
	if time.Now().Format(time.DateOnly) == message.Timestamp.Format(time.DateOnly) {
		timestampLabel = gtk.NewLabel(message.Timestamp.Format(time.TimeOnly))
	} else {
		timestampLabel = gtk.NewLabel(message.Timestamp.Format(time.DateTime))
	}
	timestampLabel.SetHAlign(gtk.AlignEnd)
	uiBottom.Append(timestampLabel)

	msg := messageRowUi{
		ListBoxRow: gtk.NewListBoxRow(),
		parent:     parent,
		message:    message,
		text:       text,
		ui:         ui,
		uiTop:      uiTop,
		uiBottom:   uiBottom,
		timestamp:  timestampLabel,
	}
	msg.ListBoxRow.SetChild(ui)
	msg.ListBoxRow.SetSelectable(false)
	msg.ListBoxRow.ConnectRealize(msg.handleRowVisible)
	msg.ListBoxRow.SetHExpand(true)
	msg.ListBoxRow.SetVExpand(true)

	return &msg
}

func (mr *messageRowUi) handleError(err error) {
	if err == nil {
		return
	}
	mr.parent.QueueMessage(ErrorView, err)
}

func (mr *messageRowUi) handleRowVisible() {
	if mr.loaded {
		return
	}

	mr.loadMediaContent()
}

func getMedia(ctx context.Context, mediaUrl string) (*[]byte, error) {
	fmt.Printf("Get %s\n", mediaUrl)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaUrl, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Got %d bytes\n", resp.ContentLength)

	return &respBytes, nil
}

func (mr *messageRowUi) updateMediaContent(mediaType models.MessageType, mediaBytes []byte) error {

	switch mediaType {
	case models.MessageTypeImage:
		fmt.Println("Loading message image from URL")
		mediaGlibBytes := glib.NewBytes(mediaBytes)
		mediaTexture, err := gdk.NewTextureFromBytes(mediaGlibBytes)
		if err != nil {
			return err
		}
		mediaImage := gtk.NewImageFromPaintable(mediaTexture)
		mr.uiTop.Prepend(mediaImage)

	case models.MessageTypeVideo:
		fmt.Println("Video messages not implemented")
	case models.MessageTypeAudio:
		fmt.Println("Audio messages not implemented")
	case models.MessageTypeDocument:
		fmt.Println("Document messages not implemented")
	}
	mr.loaded = true

	return nil
}

func (mr *messageRowUi) loadMediaContent() {
	if mr.loaded {
		return
	}

	message := mr.message
	if message == nil {
		return
	}

	if message.Type != models.MessageTypeText && message.Type != models.MessageTypeUnknown {
		if message.MediaBytes != nil {
			fmt.Println("Trying to load media from embedded bytes")
			err := mr.updateMediaContent(message.Type, *message.MediaBytes)
			if err != nil {
				fmt.Printf("Unable to update embedded media content: %s", err.Error())
			}
		}
		if !mr.loaded && message.URL != nil {
			fmt.Println("Trying to load media from URL")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			mediaBytes, err := getMedia(ctx, *message.URL)
			if err != nil {
				fmt.Printf("Unable to get media from URL: %s", err.Error())
			}
			err = mr.updateMediaContent(message.Type, *mediaBytes)
			if err != nil {
				fmt.Printf("Unable to update media content from URL: %s", err.Error())
			}
		}
	}

	if len(message.Message) > 0 {
		glib.IdleAdd(func() {
			mr.text.SetText(message.Message)
		})
	}
}

type ChatUiView struct {
	*gtk.Box
	scrolledUi  *gtk.ScrolledWindow
	parent      UiParent
	composeUi   *gtk.Box
	composeText *gtk.Text
	send        *gtk.Button
	messageList *gtk.ListBox
	messageRows []*messageRowUi
	chat        *models.ConversationModel
	topRow      *messageRowUi
	bottomRow   *messageRowUi

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

	ui := gtk.NewBox(gtk.OrientationVertical, 5)
	v.Box = ui

	v.messageList = gtk.NewListBox()
	v.messageList.SetSelectionMode(gtk.SelectionNone)
	v.messageList.SetVExpand(true)
	v.messageList.SetHExpand(true)

	viewport := gtk.NewViewport(nil, nil)
	viewport.SetScrollToFocus(true)
	viewport.SetChild(v.messageList)

	v.scrolledUi = gtk.NewScrolledWindow()
	v.scrolledUi.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	v.scrolledUi.SetChild(viewport)
	v.scrolledUi.SetPropagateNaturalHeight(true)
	v.scrolledUi.SetChild(v.messageList)
	v.Box.Append(v.scrolledUi)

	composeUi := gtk.NewBox(gtk.OrientationHorizontal, 5)
	v.composeUi = composeUi
	v.Box.Append(composeUi)

	v.composeText = gtk.NewText()
	v.composeText.SetHExpand(true)
	v.composeUi.Append(v.composeText)

	v.send = gtk.NewButtonWithLabel("Send")
	v.send.SetHAlign(gtk.AlignEnd)
	v.send.ConnectClicked(v.handleSendClicked)
	v.composeUi.Append(v.send)

	return &v
}

func (ch *ChatUiView) Title() string {
	return "WhatsWhat - Chat"
}

func (ch *ChatUiView) Done() <-chan struct{} {
	return ch.ctx.Done()
}

func (ch *ChatUiView) Close() {
	chat := ch.parent.GetChatClient()
	if ch.evtHandle != 0 && chat != nil {
		chat.RemoveEventHandler(ch.evtHandle)
	}
	if ch.cancel != nil {
		ch.cancel()
	}
	ch.chat = nil
	ch.ClearMessages(nil, nil)
	ch.composeText.SetText("")
}

func (ch *ChatUiView) chatEventHandler(evt interface{}) {
	switch evt.(type) {
	case *events.Connected:
		fmt.Println("ChatUiView.chatEventHandler: Connected")
		ch.parent.QueueMessage(ChatListView, nil)

	case *events.Disconnected:
		fmt.Println("ChatUiView.chatEventHandler: Disconnected")
		ch.Close()

	case *events.LoggedOut:
		fmt.Println("ChatUiView.chatEventHandler: Logged out")
		ch.Close()
	}
}

func (ch *ChatUiView) Update(msg *UiMessage) error {
	fmt.Println("ChatUiView.Update: Invoked")

	if msg.Error != nil {
		return msg.Error
	}

	client := ch.parent.GetChatClient()
	if client == nil || !client.IsConnected() {
		return nil
	}
	if ch.evtHandle != 0 {
		ch.Close()
	}

	switch t := msg.Payload.(type) {
	case *models.ConversationModel:
		ch.chat = t
	default:
		return fmt.Errorf("Unable to handle message payload: %s", t)
	}

	ch.ctx, ch.cancel = context.WithDeadline(context.Background(), time.Now().Add(2*time.Second))
	defer ch.cancel()

	// Bind the event handler
	ch.evtHandle = client.AddEventHandler(ch.chatEventHandler)

	if !client.IsLoggedIn() {
		return nil
	}

	// Fetch initial chat messages
	now := time.Now()
	ch.LoadMessages(nil, &now, 30)

	return nil
}

func (ch *ChatUiView) ClearMessages(startTimestamp *time.Time, endTimestamp *time.Time) {
	if startTimestamp == nil && endTimestamp == nil {
		ch.messageList.RemoveAll()
		ch.topRow = nil
		ch.bottomRow = nil
		return
	}

	var startIndex int
	var endIndex int
	for i, messageRow := range ch.messageRows {
		ts := messageRow.Timestamp()
		if startTimestamp != nil {
			if ts.Before(*startTimestamp) {
				// Keep iterating
				continue
			}
		}
		if i < startIndex {
			startIndex = i
		}
		if endTimestamp != nil {
			if ts.After(*endTimestamp) {
				// Chronological order, shouldn't occur after
				endIndex = i
				break
			}
		}
	}

	// Remove all ui elements for these rows
	for _, row := range ch.messageRows[startIndex:endIndex] {
		ch.messageList.Remove(row)
	}

	ch.messageRows = ch.messageRows[startIndex : endIndex+1]

	topRow := ch.messageList.RowAtY(0)
	if topRow == nil || topRow.Index() < 0 || topRow.Index() >= len(ch.messageRows) {
		ch.topRow = ch.messageRows[topRow.Index()]
	} else {
		ch.topRow = ch.messageRows[topRow.Index()]
	}

	bottomRow := ch.messageList.RowAtY(ch.Box.Height())
	if bottomRow == nil || bottomRow.Index() < 0 || bottomRow.Index() >= len(ch.messageRows) {
		ch.bottomRow = nil
	} else {
		ch.bottomRow = ch.messageRows[bottomRow.Index()]
	}
}

func (ch *ChatUiView) LoadMessages(startTimestamp *time.Time, endTimestamp *time.Time, limit int) {
	client := ch.parent.GetChatClient()
	deviceJID := client.Store.ID
	chatDB := ch.parent.GetChatDB()

	messages, err := chatDB.Message.GetBetween(ch.ctx, *deviceJID, ch.chat.ChatJID, startTimestamp, endTimestamp, limit)
	if err != nil {
		ch.parent.QueueMessage(ErrorView, err)
		return
	}

	fmt.Printf("Got %d messages in chat %s for device %s", len(messages), ch.chat.ChatJID, deviceJID)

	// Convert messages to models
	msgModels := make([]*models.MessageModel, len(messages))
	for i, message := range messages {
		model, err := models.GetMessageModel(client, ch.chat.ChatJID, message)
		if err != nil {
			ch.parent.QueueMessage(ErrorView, err)
			return
		}
		msgModels[i] = model
	}

	msgRows := make([]*messageRowUi, len(messages))
	for i, model := range msgModels {
		msgRows[i] = NewMessageRowUi(ch.ctx, ch.parent, model)
		// TODO
		ch.messageList.Prepend(msgRows[i])
	}

	// TODO: before / after or intersperse. Must be cronological
	ch.messageRows = append(ch.messageRows, msgRows...)

	fmt.Println("ChatUiView.LoadMessages completed")
}

func (ch *ChatUiView) handleSendClicked() {
	msgText := strings.Clone(ch.composeText.Text())
	fmt.Printf("Send event: %s\n", msgText)
}
