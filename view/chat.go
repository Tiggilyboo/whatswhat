package view

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gdkpixbuf/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
	"github.com/tiggilyboo/whatswhat/view/models"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
)

type messageRowUi struct {
	*gtk.ListBoxRow
	parent     UiParent
	message    *models.MessageModel
	ui         *gtk.Box
	uiTop      *gtk.Box
	uiBottom   *gtk.Box
	uiMedia    *gtk.Box
	timestamp  *gtk.Label
	text       *gtk.Label
	sender     *gtk.Label
	loadedChan chan int
	loaded     bool
}

func (mr *messageRowUi) Timestamp() time.Time {
	return mr.message.Timestamp
}

func NewMessageRowUi(ctx context.Context, parent UiParent, message *models.MessageModel, loadedChan chan int) *messageRowUi {
	ui := gtk.NewBox(gtk.OrientationVertical, 0)
	uiTop := gtk.NewBox(gtk.OrientationHorizontal, 5)
	uiTop.SetMarginEnd(10)
	uiTop.SetMarginStart(10)
	uiTop.SetHExpand(true)

	uiMedia := gtk.NewBox(gtk.OrientationHorizontal, 5)
	uiMedia.SetHExpand(true)

	uiBottom := gtk.NewBox(gtk.OrientationHorizontal, 5)
	uiBottom.SetHExpand(true)

	ui.SetHExpand(true)
	ui.SetVExpand(true)
	ui.Append(uiTop)
	ui.Append(uiMedia)
	ui.Append(uiBottom)

	var senderText string
	if message.IsFromMe {
		senderText = "Me"
	} else if len(message.PushName) > 0 {
		senderText = message.PushName
	} else {
		contacts, err := parent.GetContacts()
		if err != nil {
			if !message.IsGroup {
				senderText = message.SenderJID.User
			}
		} else {
			senderContact, ok := contacts[message.SenderJID.ToNonAD()]
			if !ok {
				senderText = message.SenderJID.User
			} else {
				senderText = senderContact.FullName
			}
		}
	}
	senderMarkup := fmt.Sprintf("<a href=\"%d/%s\">%s</a>", ProfileView, message.SenderJID, senderText)
	sender := gtk.NewLabel(senderMarkup)
	sender.SetUseMarkup(true)
	sender.ConnectActivateLink(func(uri string) bool {
		parent.QueueMessage(ProfileView, message.SenderJID.ToNonAD())
		return true
	})
	sender.SetHAlign(gtk.AlignStart)
	sender.SetVAlign(gtk.AlignStart)
	sender.SetHExpand(true)
	uiTop.Append(sender)

	text := gtk.NewLabel("...")
	text.SetUseMarkup(true)
	text.SetHAlign(gtk.AlignStart)
	text.SetVAlign(gtk.AlignStart)
	text.SetHExpand(true)
	text.SetWrap(true)
	text.SetSelectable(true)
	text.SetWrapMode(pango.WrapWordChar)
	uiBottom.Append(text)

	var timestampLabel *gtk.Label
	if time.Now().Format(time.DateOnly) == message.Timestamp.Format(time.DateOnly) {
		timestampLabel = gtk.NewLabel(message.Timestamp.Format(time.TimeOnly))
	} else {
		timestampLabel = gtk.NewLabel(message.Timestamp.Format(time.DateTime))
	}
	timestampLabel.SetSensitive(false)
	timestampLabel.SetHAlign(gtk.AlignEnd)
	uiTop.Append(timestampLabel)

	msg := messageRowUi{
		ListBoxRow: gtk.NewListBoxRow(),
		parent:     parent,
		message:    message,
		text:       text,
		ui:         ui,
		uiTop:      uiTop,
		uiBottom:   uiBottom,
		uiMedia:    uiMedia,
		timestamp:  timestampLabel,
		sender:     sender,
		loadedChan: loadedChan,
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

	go mr.loadRowContent()
}

func (mr *messageRowUi) updateMediaContent(mediaBytes []byte) error {
	mime := mr.message.Media.GetMimetype()
	fmt.Printf("Updating UI with bytes of mime type: %s\n", mime)

	switch mr.message.MsgType {
	case models.MessageTypeImage:
		mediaGlibBytes := glib.NewBytes(mediaBytes)
		mediaTexture, err := gdk.NewTextureFromBytes(mediaGlibBytes)
		if err != nil {
			return err
		}

		// resize texture to bound to screen width
		w, h := mr.parent.GetWindowSize()

		imageWidth := mediaTexture.Width()
		imageHeight := mediaTexture.Height()
		if mediaWithDims, ok := mr.message.Media.(models.MediaMessageWithDimensions); ok {
			imageWidth = int(mediaWithDims.GetWidth())
			imageHeight = int(mediaWithDims.GetHeight())
		}
		fmt.Printf("Loading image with dimensions: %d x %d into window with %d x %d", imageWidth, imageHeight, w, h)

		if imageWidth > w-20 {
			scale := float32(w) / float32(imageWidth)
			scaledH := scale * float32(imageHeight)
			h = int(scaledH)
			fmt.Printf("Resizing image to %d x %d\n", w, h)

			if w > 0 && h > 0 {
				mediaPixbuf := gdk.PixbufGetFromTexture(mediaTexture)
				mediaPixbuf = mediaPixbuf.ScaleSimple(w, h, gdkpixbuf.InterpBilinear)
				mediaTexture = gdk.NewTextureForPixbuf(mediaPixbuf)
				fmt.Printf("Resized image to %d x %d\n", w, h)
			} else {
				fmt.Printf("Unable to find reasonable image dimensions, not scaling\n")
			}
		}

		mediaImage := gtk.NewImageFromPaintable(mediaTexture)
		mediaImage.SetSizeRequest(w, h)
		mediaImage.SetHExpand(true)
		mediaImage.SetVExpand(true)

		mr.uiMedia.Append(mediaImage)

	case models.MessageTypeVideo:
		fmt.Println("Video messages not implemented")
	case models.MessageTypeAudio:
		fmt.Println("Audio messages not implemented")
	case models.MessageTypeDocument:
		fmt.Println("Document messages not implemented")
	}

	return nil
}

func (mr *messageRowUi) loadRowContent() {
	if mr.loaded {
		return
	}

	message := mr.message
	if message == nil {
		return
	}

	if !mr.loaded && message.Media != nil {
		fmt.Println("Trying to load media from URL")

		mediaBytes, err := mr.parent.GetChatClient().Download(message.Media)
		if err != nil {
			fmt.Printf("Unable to get media from URL: %s", err.Error())
		}
		err = mr.updateMediaContent(mediaBytes)
		if err != nil {
			fmt.Printf("Unable to update media content from URL: %s", err.Error())
		}
	}

	if len(message.Message) > 0 {
		glib.IdleAdd(func() {
			mr.text.SetMarkup(message.Message)
		})
	}

	mr.loaded = true
	idx := mr.ListBoxRow.Index()
	mr.loadedChan <- idx
}

type ChatUiView struct {
	*gtk.Box
	viewport          *gtk.Viewport
	scrolledUi        *gtk.ScrolledWindow
	parent            UiParent
	composeUi         *gtk.Box
	composeText       *gtk.Entry
	send              *gtk.Button
	messageList       *gtk.ListBox
	messageRows       []*messageRowUi
	chat              *models.ConversationModel
	messagePending    *messageRowUi
	customScrolled    bool
	customScrollIndex int
	messageLoaded     chan int

	ctx       context.Context
	cancel    context.CancelFunc
	evtHandle uint32
}

func NewChatView(parent UiParent) *ChatUiView {
	ctx, cancel := context.WithCancel(context.Background())

	v := ChatUiView{
		ctx:           ctx,
		cancel:        cancel,
		parent:        parent,
		messageLoaded: make(chan int),
	}
	context.AfterFunc(ctx, v.Close)

	ui := gtk.NewBox(gtk.OrientationVertical, 5)
	v.Box = ui

	v.messageList = gtk.NewListBox()
	v.messageList.SetSelectionMode(gtk.SelectionNone)
	v.messageList.SetVExpand(true)
	v.messageList.SetHExpand(true)

	v.viewport = gtk.NewViewport(nil, v.messageList.Adjustment())
	v.viewport.SetChild(v.messageList)
	v.viewport.SetScrollToFocus(true)
	v.viewport.SetVScrollPolicy(gtk.ScrollablePolicy(gtk.ScrollEnd))

	v.scrolledUi = gtk.NewScrolledWindow()
	v.scrolledUi.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	v.scrolledUi.SetChild(v.viewport)
	v.scrolledUi.SetPropagateNaturalHeight(true)
	v.scrolledUi.SetChild(v.messageList)
	v.scrolledUi.ConnectEdgeOvershot(v.handleMessageListEdgeOvershot)
	v.Box.Append(v.scrolledUi)

	composeUi := gtk.NewBox(gtk.OrientationHorizontal, 0)
	v.composeUi = composeUi
	v.Box.Append(composeUi)

	v.composeText = gtk.NewEntry()
	v.composeText.SetHExpand(true)
	v.SetSizeRequest(-1, -1)
	v.composeUi.Append(v.composeText)

	v.send = gtk.NewButtonWithLabel("Send")
	v.send.ConnectClicked(v.handleSendClicked)
	v.composeUi.Append(v.send)

	go v.consumeMessageLoadedChannel()

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
	ch.customScrolled = false
	ch.customScrollIndex = 0
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

func (ch *ChatUiView) consumeMessageLoadedChannel() {
	for rowIndex := range ch.messageLoaded {
		if rowIndex < 0 || rowIndex >= len(ch.messageRows) {
			continue
		}
		var messageRow *messageRowUi
		if !ch.customScrolled {
			fmt.Printf("Message loaded, scrolling to last row\n")
			messageRow = ch.messageRows[len(ch.messageRows)-1]
		} else {
			if ch.customScrollIndex < 0 || ch.customScrollIndex >= len(ch.messageRows) {
				fmt.Printf("Custom scroll index out of range: %d\n", ch.customScrollIndex)
				continue
			}

			fmt.Printf("Message loaded, scrolling to row %d\n", ch.customScrollIndex)
			messageRow = ch.messageRows[ch.customScrollIndex]
		}

		ch.scrolledUi.SetFocusChild(ch.messageList)
		ch.messageList.SetFocusChild(messageRow)
		ch.viewport.ScrollTo(messageRow, nil)
	}
}

func (ch *ChatUiView) Update(msg *UiMessage) (Response, error) {
	fmt.Println("ChatUiView.Update: Invoked")

	client := ch.parent.GetChatClient()
	if client == nil || !client.IsConnected() {
		return msg.Intent, nil
	}
	var beforeTime time.Time

	switch t := msg.Payload.(type) {
	case *models.ConversationModel:
		if ch.chat != nil && ch.chat.ChatJID != t.ChatJID {
			ch.Close()
		}
		ch.chat = t
		ch.ClearMessages(nil, nil)
		ch.composeText.SetText("")
		beforeTime = time.Now()

	case *models.MessageModel:
		if ch.chat.ChatJID == t.ChatJID {
			uiRow := NewMessageRowUi(ch.ctx, ch.parent, t, ch.messageLoaded)

			if len(ch.messageRows) == 0 {
				ch.messageRows = append(ch.messageRows, uiRow)
				ch.messageList.Append(uiRow)
			}

			if t.Timestamp.Before(ch.messageRows[0].Timestamp()) {
				// prepend
				ch.messageList.Prepend(uiRow)
				ch.messageRows = append([]*messageRowUi{uiRow}, ch.messageRows...)

			} else if t.Timestamp.After(ch.messageRows[len(ch.messageRows)-1].Timestamp()) {
				// append
				ch.messageList.Append(uiRow)
				ch.messageRows = append(ch.messageRows, uiRow)

			} else {
				// insert
				insertIdx := ch.binarySearchInsert(uiRow)
				insertIdx = len(ch.messageRows) - insertIdx
				newMessagesRows := make([]*messageRowUi, len(ch.messageRows)+1)
				newMessagesRows[insertIdx] = uiRow
				copy(newMessagesRows[0:insertIdx], ch.messageRows[0:insertIdx])
				copy(newMessagesRows[insertIdx+1:], ch.messageRows[insertIdx:])
				ch.messageRows = newMessagesRows
				ch.messageList.Insert(uiRow, insertIdx)
			}

			return ResponseIgnore, nil
		}
	case *time.Time:
		// Set the period to load FROM
		beforeTime = *t
	case nil:
		// Don't do anything, remain in the same chat

	default:
		return ResponsePushView, fmt.Errorf("Unable to handle message payload: %s", t)
	}

	ch.ctx, ch.cancel = context.WithTimeout(context.Background(), 2*time.Second)

	// Bind the event handler
	ch.evtHandle = client.AddEventHandler(ch.chatEventHandler)

	if !client.IsLoggedIn() {
		return msg.Intent, nil
	}

	// Fetch initial chat messages
	go ch.LoadMessages(nil, &beforeTime, 30)

	return msg.Intent, nil
}

func (ch *ChatUiView) ClearMessages(startTimestamp *time.Time, endTimestamp *time.Time) {
	if startTimestamp == nil && endTimestamp == nil {
		ch.messageList.RemoveAll()
		ch.messageRows = make([]*messageRowUi, 0)
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
}

func (ch *ChatUiView) binarySearchInsert(message *messageRowUi) int {
	low := 0
	high := len(ch.messageRows) - 1
	target := message.Timestamp()

	for low <= high {
		mid := (low + high) / 2
		if ch.messageRows[mid].Timestamp() == target {
			return mid
		} else if ch.messageRows[mid].Timestamp().Before(target) {
			low = mid + 1
		} else {
			high = mid - 1
		}
	}

	return low
}

func (ch *ChatUiView) LoadMessages(startTimestamp *time.Time, endTimestamp *time.Time, limit int) {
	if ch.cancel != nil {
		defer ch.cancel()
	}

	client := ch.parent.GetChatClient()
	chatDB := ch.parent.GetChatDB()
	deviceJID := ch.parent.GetDeviceJID()
	if deviceJID == nil || chatDB == nil || client == nil {
		return
	}

	messages, err := chatDB.Message.GetBetween(ch.ctx, *deviceJID, ch.chat.ChatJID, startTimestamp, endTimestamp, limit)
	if err != nil {
		ch.parent.QueueMessage(ErrorView, err)
		return
	}

	fmt.Printf("Got %d messages in chat %s for device %s", len(messages), ch.chat.ChatJID, deviceJID)

	// Convert messages to models
	msgModels := make([]*models.MessageModel, len(messages))
	for i, message := range messages {

		// convert to message event
		msgEvent, err := client.ParseWebMessage(ch.chat.ChatJID, message)
		if err != nil {
			ch.parent.QueueMessage(ErrorView, fmt.Errorf("Unable to parse web message for chat: %v", err))
			return
		}

		// convert to UI model
		model, err := models.GetMessageModel(client, ch.chat.ChatJID, msgEvent)
		if err != nil {
			ch.parent.QueueMessage(ErrorView, fmt.Errorf("Unable to convert message event to message model: %v", err))
			return
		}
		msgModels[i] = model
	}

	// Insert messages into the existing messages (if any) to keep chronological order
	i := 0
	j := 0
	merged := make([]*messageRowUi, len(ch.messageRows)+len(messages))
	lenOrig := len(ch.messageRows)
	lenInsert := len(msgModels)

	for i < lenOrig && j < lenInsert {
		if ch.messageRows[i].Timestamp().After(msgModels[j].Timestamp) {
			merged[i+j] = ch.messageRows[i]
			i++
		} else {
			newRow := NewMessageRowUi(ch.ctx, ch.parent, msgModels[j], ch.messageLoaded)
			merged[i+j] = newRow
			j++

			ch.messageList.Insert(newRow, i+j)
		}
	}
	for i < lenOrig {
		merged[i+j] = ch.messageRows[i]
		i++
	}
	for j < lenInsert {
		newRow := NewMessageRowUi(ch.ctx, ch.parent, msgModels[j], ch.messageLoaded)
		merged[i+j] = newRow
		j++

		ch.messageList.Prepend(newRow)
	}
	ch.messageRows = merged

	fmt.Println("ChatUiView.LoadMessages completed")
}

func (ch *ChatUiView) handleMessageListEdgeOvershot(posType gtk.PositionType) {
	var newestTimestamp time.Time
	var oldestTimestamp time.Time
	if len(ch.messageRows) > 1 {
		oldestTimestamp = ch.messageRows[0].Timestamp()
		newestTimestamp = ch.messageRows[len(ch.messageRows)-1].Timestamp()
	} else {
		newestTimestamp = time.Now()
		oldestTimestamp = time.Time{}
	}

	if posType == gtk.PosTop {
		// Load new messages in history from the oldest loaded message timestamp
		fmt.Printf("Got request to load older messages before %s\n", oldestTimestamp.String())

		// Wait for any messages we've already tried to load to complete
		<-ch.ctx.Done()

		// Start a new context to load
		ch.ctx, ch.cancel = context.WithTimeout(context.Background(), 3*time.Second)
		go ch.LoadMessages(nil, &oldestTimestamp, 30)

	} else if posType == gtk.PosBottom {
		// Load new messages in history from the newest loaded message timestamp
		fmt.Printf("Got request to load messages between %s and %s\n", oldestTimestamp.String(), newestTimestamp.String())

		// Wait for any messages we've already tried to load to complete
		<-ch.ctx.Done()

		// Start a new context to load
		ch.ctx, ch.cancel = context.WithTimeout(context.Background(), 3*time.Second)
		now := time.Now()
		go ch.LoadMessages(&newestTimestamp, &now, 30)
	}
}

func (ch *ChatUiView) handleSendClicked() {

	// Check for existing pending message that is being sent
	// There can only be one
	if ch.messagePending != nil {
		return
	}

	deviceJID := ch.parent.GetDeviceJID()
	if deviceJID == nil {
		fmt.Printf("DeviceJID empty when trying to send message\n")
		ch.parent.QueueMessage(ChatListView, nil)
		return
	}

	// Cancel any existing context
	if ch.cancel != nil {
		ch.cancel()
	}

	ch.send.SetLabel("Sending...")
	ch.composeText.SetEditable(false)

	// Start a new one for the sending action
	ch.ctx, ch.cancel = context.WithTimeout(context.Background(), 30*time.Second)
	context.AfterFunc(ch.ctx, func() {
		ch.finishPendingMessage()
	})

	msgText := strings.Clone(ch.composeText.Text())
	fmt.Printf("Send event: %s\n", msgText)

	msgModel := models.NewPendingMessage("Pending", ch.chat.ChatJID, *deviceJID, "Me", msgText)
	pending := NewMessageRowUi(ch.ctx, ch.parent, &msgModel, ch.messageLoaded)

	// Append the pending message to the list
	ch.messageList.Append(pending)
	ch.messageRows = append(ch.messageRows, pending)
	ch.messagePending = pending

	go ch.sendPendingMessage(ch.ctx, ch.cancel, &msgModel)
}

func (ch *ChatUiView) finishPendingMessage() {

	// Reset UI
	ch.send.SetLabel("Send")
	ch.composeText.SetEditable(true)

	if ch.messagePending == nil {
		return
	}
	// Sent?
	if ch.messagePending.message.ID != "Pending" {
		ch.composeText.SetText("")
		ch.messagePending = nil
		return
	}

	// Remove it, timed out
	fmt.Printf("Removing pending message, timed out\n")
	lastRow := ch.messageRows[len(ch.messageRows)-1]
	if lastRow != ch.messagePending {
		ch.parent.QueueMessage(ErrorView, fmt.Errorf("Pending message %s should be at end of chat, but was not!"))
		return
	}
	ch.messageRows = ch.messageRows[:len(ch.messageRows)-2]
	ch.messageList.Remove(ch.messagePending)

	ch.parent.QueueMessage(ErrorView, fmt.Errorf("Timed out sending message"))
}

func (ch *ChatUiView) sendPendingMessage(ctx context.Context, cancel context.CancelFunc, msg *models.MessageModel) {
	defer cancel()

	if ch.messagePending == nil {
		// TODO: error
		return
	}
	pending := ch.messagePending
	message := waE2E.Message{
		Conversation: &msg.Message,
	}
	sendReq, err := ch.parent.GetChatClient().SendMessage(ctx, ch.chat.ChatJID, &message)
	if err == nil {
		pending.message.ID = sendReq.ID
		pending.message.Timestamp = sendReq.Timestamp
		fmt.Printf("Sent message\n")
	} else {
		fmt.Printf("Error sending message: %s\n", err.Error())
		return
	}
}
