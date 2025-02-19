package view

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gdkpixbuf/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
	"github.com/tiggilyboo/whatswhat/db"
	"github.com/tiggilyboo/whatswhat/view/models"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"

	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type statusRowUi struct {
	*gtk.ListBoxRow
	parent   UiParent
	ui       *gtk.Box
	spinner  *gtk.Spinner
	button   *gtk.Button
	status   *gtk.Label
	feedback chan interface{}
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewStatusRowUi(parent UiParent, buttonLabel string, initialStatus string) *statusRowUi {
	ui := gtk.NewBox(gtk.OrientationVertical, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rowUi := statusRowUi{
		ListBoxRow: gtk.NewListBoxRow(),
		spinner:    gtk.NewSpinner(),
		button:     gtk.NewButtonWithLabel(buttonLabel),
		status:     gtk.NewLabel(initialStatus),
		feedback:   make(chan interface{}),
		ctx:        ctx,
		cancel:     cancel,
	}
	rowUi.SetVisible(false)

	rowUi.spinner.SetVisible(false)
	rowUi.spinner.SetHAlign(gtk.AlignCenter)
	rowUi.spinner.SetVAlign(gtk.AlignCenter)

	rowUi.status.SetVisible(false)
	rowUi.status.SetHAlign(gtk.AlignCenter)
	rowUi.status.SetVAlign(gtk.AlignCenter)

	rowUi.button.SetVisible(false)

	ui.SetHExpand(true)
	ui.SetVExpand(true)
	ui.Append(rowUi.status)
	ui.Append(rowUi.spinner)
	ui.Append(rowUi.button)

	rowUi.ListBoxRow.SetChild(ui)

	go rowUi.consumeFeedback()

	return &rowUi
}

func (sr *statusRowUi) SetLoading(status string) {
	sr.SetVisible(true)
	sr.button.SetVisible(false)
	sr.spinner.SetVisible(true)
	sr.status.SetLabel(status)
	sr.status.SetVisible(true)
}

func (sr *statusRowUi) SetLoaded() {
	sr.SetVisible(true)
	sr.button.SetVisible(true)
	sr.spinner.SetVisible(false)
	sr.status.SetVisible(false)
}

func (sr *statusRowUi) SetStatus(status string) {
	sr.SetVisible(true)
	sr.button.SetVisible(false)
	sr.spinner.SetVisible(false)
	sr.status.SetVisible(true)
	sr.status.SetLabel(status)
}

func (sr *statusRowUi) consumeFeedback() {
	for feedback := range sr.feedback {
		defer sr.cancel()

		switch v := feedback.(type) {
		case error:
			sr.SetStatus(v.Error())
		case string:
			sr.SetStatus(v)
		case nil:
			sr.SetLoaded()
		default:
			sr.SetStatus(fmt.Sprintf("Unhandled status: %v", v))
		}
	}
}

type messageRowUi struct {
	*gtk.ListBoxRow
	parent         UiParent
	message        *models.MessageModel
	ui             *gtk.Box
	uiTop          *gtk.Box
	uiBottom       *gtk.Box
	uiMedia        *gtk.Box
	uiMediaWidgets []gtk.Widgetter
	timestamp      *gtk.Label
	text           *gtk.Label
	sender         *gtk.Label
	loadedChan     chan int
	loaded         bool
	downloaded     bool
	lock           sync.RWMutex
}

func (mr *messageRowUi) Timestamp() time.Time {
	if mr.message == nil {
		return time.Time{}
	}
	return mr.message.Timestamp
}

func NewMessageRowUi(parent UiParent, message *models.MessageModel, loadedChan chan int) *messageRowUi {
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
		pushNames, err := parent.GetPushNames()
		if err == nil {
			if pushName, ok := pushNames[message.SenderJID]; ok {
				senderText = pushName.Name
			}
		}
		if len(senderText) == 0 {
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

func (mr *messageRowUi) downloadMediaContent() ([]byte, error) {
	mediaBytes, err := mr.parent.GetChatClient().Download(mr.message.Media)
	if err != nil {
		fmt.Printf("Unable to get media: %s", err.Error())
		return nil, err
	}
	return mediaBytes, nil
}

func (mr *messageRowUi) populateMediaContent() {
	fmt.Printf("populateMediaContent: loaded %v, downloaded: %v, msgID: %s", mr.loaded, mr.downloaded, mr.message.ID)
	if !mr.loaded {
		return
	}
	if mr.downloaded {
		return
	}

	fmt.Printf("populateMediaContent: locking")
	mr.lock.Lock()
	fmt.Printf("populateMediaContent: locked")

	if mr.uiMediaWidgets != nil {
		for _, widget := range mr.uiMediaWidgets {
			mr.uiMedia.Remove(widget)
		}
	}
	spinner := gtk.NewSpinner()
	spinner.SetSpinning(true)
	label := gtk.NewLabel("Downloading...")

	mr.uiMediaWidgets = []gtk.Widgetter{label, spinner}
	mr.uiMedia.Append(label)
	mr.uiMedia.Append(spinner)

	mr.lock.Unlock()
	fmt.Printf("populateMediaContent: unlocked")

	mediaBytes, err := mr.downloadMediaContent()
	if err != nil {
		label.SetLabel(fmt.Sprintf("Error downloading: %s", err))
		spinner.SetVisible(false)
		return
	}

	err = mr.updateMediaContent(mediaBytes)
	if err != nil {
		label.SetLabel(fmt.Sprintf("Error updating media: %s", err))
		return
	}

	mr.lock.Lock()
	mr.downloaded = true
	mr.lock.Unlock()
	fmt.Printf("populateMediaContent: done, downloaded")
}

func (mr *messageRowUi) getScaledMediaDimensions() (int, int) {
	imageWidth, imageHeight := 0, 0
	w, h := mr.parent.GetWindowSize()
	if mediaWithDims, ok := mr.message.Media.(models.MediaMessageWithDimensions); ok {
		imageWidth = int(mediaWithDims.GetWidth())
		imageHeight = int(mediaWithDims.GetHeight())
	}
	fmt.Printf("Loading image with dimensions: %d x %d into window with %d x %d", imageWidth, imageHeight, w, h)
	if imageWidth > w-20 {
		scale := float32(w) / float32(imageWidth)
		scaledH := scale * float32(imageHeight)
		h = int(scaledH)
	}

	return w, h

}

func (mr *messageRowUi) updateMediaContent(mediaBytes []byte) error {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	if mr.uiMediaWidgets != nil {
		fmt.Println("Removing existing media widgets: %d\n", len(mr.uiMediaWidgets))
		for _, widget := range mr.uiMediaWidgets {
			if widget == nil {
				continue
			}
			mr.uiMedia.Remove(widget)
		}
	}
	mr.uiMediaWidgets = make([]gtk.Widgetter, 0)

	switch mr.message.MsgType {
	case models.MessageTypeImage:
		w, h := mr.getScaledMediaDimensions()

		var mediaWidget gtk.Widgetter
		if mediaBytes != nil {
			fmt.Printf("Loading %d image bytes\n", len(mediaBytes))

			mediaGlibBytes := glib.NewBytes(mediaBytes)
			mediaTexture, err := gdk.NewTextureFromBytes(mediaGlibBytes)
			if err != nil {
				return err
			}
			fmt.Printf("Getting pixbuf from texture")
			mediaPixbuf := gdk.PixbufGetFromTexture(mediaTexture)
			fmt.Printf("ScaleSimple: %d x %d\n", w, h)
			mediaPixbuf = mediaPixbuf.ScaleSimple(w, h, gdkpixbuf.InterpBilinear)
			mediaTexture = gdk.NewTextureForPixbuf(mediaPixbuf)
			fmt.Printf("Resized image to %d x %d\n", w, h)
			if w <= 0 || h <= 0 {
				w = mediaTexture.IntrinsicWidth()
				h = mediaTexture.IntrinsicHeight()
			}
			mediaImage := gtk.NewImage()
			mediaImage.SetFromPaintable(mediaTexture)
			mediaImage.SetSizeRequest(w, h)
			mediaWidget = mediaImage
		} else {
			mediaContainer := gtk.NewBox(gtk.OrientationHorizontal, 5)
			fileInfo := mr.message.Media.GetMimetype()
			if mediaWithFileName, ok := mr.message.Media.(models.MediaMessageWithFileName); ok {
				fileInfo = mediaWithFileName.GetFileName()
			}
			mediaSize := mr.message.MediaSize()
			if mediaSize != nil {
				fileInfo += fmt.Sprintf("%s (%s)", fileInfo, *mediaSize)
			}

			documentLabel := gtk.NewLabel(fileInfo)
			mediaContainer.Append(documentLabel)
			mediaDownloadButton := gtk.NewButtonFromIconName("folder-download-symbolic")
			mediaDownloadButton.ConnectClicked(func() {
				go mr.populateMediaContent()
			})
			mediaContainer.Append(mediaDownloadButton)
			mediaWidget = mediaContainer
		}

		mr.uiMediaWidgets = []gtk.Widgetter{mediaWidget}
		mr.uiMedia.Append(mediaWidget)

	case models.MessageTypeVideo:
		fmt.Println("Video messages not implemented")
	case models.MessageTypeAudio:
		fmt.Println("Audio messages not implemented")
	case models.MessageTypeDocument:
		if mediaBytes != nil {
			fmt.Printf("Loading %d document bytes\n", len(mediaBytes))

			return nil
		}

		uiDocument := gtk.NewBox(gtk.OrientationHorizontal, 5)
		fileInfo := mr.message.Media.GetMimetype()
		if mediaWithFileName, ok := mr.message.Media.(models.MediaMessageWithFileName); ok {
			fileInfo = mediaWithFileName.GetFileName()
		}
		fileInfo += fmt.Sprintf("%s (%s)", fileInfo, *mr.message.MediaSize())

		documentLabel := gtk.NewLabel(fileInfo)
		uiDocument.Append(documentLabel)
		documentButton := gtk.NewButtonFromIconName("folder-download-symbolic")
		documentButton.ConnectClicked(func() {
			mr.populateMediaContent()
		})
		uiDocument.Append(documentButton)

		mr.uiMediaWidgets = []gtk.Widgetter{uiDocument}
		mr.uiMedia.Append(uiDocument)
	}

	return nil
}

func (mr *messageRowUi) loadRowContent() {
	if mr == nil {
		return
	}
	if mr.loaded {
		return
	}
	if mr.downloaded {
		return
	}

	message := mr.message
	if message == nil {
		return
	}

	fmt.Printf("messageRowUi.loadRowContent")
	if !mr.loaded && message.Media != nil {
		err := mr.updateMediaContent(nil)
		if err != nil {
			fmt.Printf("Unable to update media content UI: %s", err.Error())
		}
	}

	mr.lock.Lock()
	defer mr.lock.Unlock()

	if len(message.Message) > 0 {
		mr.text.SetMarkup(message.Message)
	}
	mr.loaded = true

	idx := mr.ListBoxRow.Index()
	mr.loadedChan <- idx
	fmt.Printf("messageRowUi.loadRowContent: Row %d Loaded", idx)
}

type ChatUiView struct {
	*gtk.Box
	viewport            *gtk.Viewport
	scrolledUi          *gtk.ScrolledWindow
	parent              UiParent
	composeUi           *gtk.Box
	composeText         *gtk.Entry
	send                *gtk.Button
	messageList         *gtk.ListBox
	statusRow           *statusRowUi
	messageRows         []*messageRowUi
	chat                *models.ConversationModel
	messagePending      *messageRowUi
	customScrollMessage *messageRowUi
	customScrolled      bool
	closed              bool
	messageLoaded       chan int
	lock                sync.RWMutex
	ctx                 context.Context
	cancel              context.CancelFunc
	evtHandle           uint32
}

func NewChatView(parent UiParent) *ChatUiView {
	ctx, cancel := context.WithCancel(context.Background())

	v := ChatUiView{
		ctx:           ctx,
		cancel:        cancel,
		parent:        parent,
		messageLoaded: make(chan int),
		closed:        true,
		lock:          sync.RWMutex{},
	}

	ui := gtk.NewBox(gtk.OrientationVertical, 5)
	v.Box = ui

	v.messageList = gtk.NewListBox()
	v.messageList.SetSelectionMode(gtk.SelectionNone)
	v.messageList.SetVExpand(true)
	v.messageList.SetHExpand(true)

	v.statusRow = NewStatusRowUi(parent, "Load more", "")
	v.statusRow.SetVisible(false)
	v.statusRow.button.ConnectClicked(func() {
		v.LoadOlderMessages(30)
	})

	v.messageList.Append(v.statusRow)

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
	v.composeUi.Append(v.composeText)

	v.send = gtk.NewButtonWithLabel("Send")
	v.send.ConnectClicked(v.handleSendClicked)
	v.composeUi.Append(v.send)

	go v.consumeMessageLoadedChannel()

	return &v
}

func (ch *ChatUiView) newTaskWithTimeout(timeout time.Duration) {
	ch.lock.Lock()
	defer ch.lock.Unlock()

	if ch.cancel != nil {
		ch.cancel()
	}
	ch.ctx, ch.cancel = context.WithTimeout(context.Background(), timeout)
}

func (ch *ChatUiView) Title() string {
	if ch.chat != nil && len(ch.chat.Name) > 0 {
		return ch.chat.Name
	} else {
		return "Chat"
	}
}

func (ch *ChatUiView) Done() <-chan struct{} {
	return ch.ctx.Done()
}

func (ch *ChatUiView) ChatJID() types.JID {
	return ch.chat.ChatJID
}

func (ch *ChatUiView) Close() {
	fmt.Println("ChatUiView.Close: Started")
	ch.lock.Lock()
	defer ch.lock.Unlock()

	chat := ch.parent.GetChatClient()
	if ch.evtHandle != 0 && chat != nil {
		chat.RemoveEventHandler(ch.evtHandle)
	}
	if ch.cancel != nil {
		ch.cancel()
	}
	ch.customScrolled = false
	ch.customScrollMessage = nil
	ch.closed = true
	fmt.Println("ChatUiView.Close: Done")
}

func (ch *ChatUiView) chatEventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Connected:
		fmt.Println("ChatUiView.chatEventHandler: Connected")
		ch.parent.QueueMessageWithIntent(ChatListView, nil, ResponseBackView)

	case *events.Disconnected:
		fmt.Println("ChatUiView.chatEventHandler: Disconnected")
		ch.parent.QueueMessageWithIntent(ChatListView, nil, ResponseBackView)

	case *events.LoggedOut:
		fmt.Println("ChatUiView.chatEventHandler: Logged out")
		ch.parent.QueueMessageWithIntent(ChatListView, nil, ResponseBackView)
	case *events.Message:
		fmt.Println("ChatUiView got message: %v", v)
	}
}

func (ch *ChatUiView) handleScrollToMessage(rowIndex int) {
	fmt.Printf("handleScrollToMessage: %d\n", rowIndex)
	ch.lock.Lock()
	defer ch.lock.Unlock()

	if ch.messageRows == nil || len(ch.messageRows) == 0 {
		return
	}
	if rowIndex < 0 || rowIndex >= len(ch.messageRows) {
		return
	}

	var messageRow *messageRowUi
	if !ch.customScrolled {
		//fmt.Printf("Message loaded, scrolling to last row\n")
		messageRow = ch.messageRows[len(ch.messageRows)-1]
	} else {
		if ch.customScrollMessage == nil {
			return
		}

		//fmt.Printf("Message loaded, scrolling to row %v\n", ch.customScrollMessage)
		messageRow = ch.customScrollMessage
	}
	if messageRow == nil {
		return
	}
	ch.viewport.ScrollTo(messageRow, nil)
}

func (ch *ChatUiView) consumeMessageLoadedChannel() {
	for rowIndex := range ch.messageLoaded {
		ch.handleScrollToMessage(rowIndex)
	}
}

func (ch *ChatUiView) chatLoadedMarkRead(chatJID types.JID, lastMessageTimestamp time.Time, read bool) {

	// Until this is merged: https://github.com/tulir/whatsmeow/pull/691/
	if lastMessageTimestamp.IsZero() {
		lastMessageTimestamp = time.Now()
	}
	mutationInfo := appstate.MutationInfo{
		Index:   []string{appstate.IndexMarkChatAsRead, chatJID.String()},
		Version: 3,
		Value: &waSyncAction.SyncActionValue{
			MarkChatAsReadAction: &waSyncAction.MarkChatAsReadAction{
				Read: proto.Bool(read),
				MessageRange: &waSyncAction.SyncActionMessageRange{
					LastMessageTimestamp: proto.Int64(lastMessageTimestamp.Unix()),
				},
			},
		},
	}
	patchInfo := appstate.PatchInfo{
		Type:      appstate.WAPatchRegularLow,
		Mutations: []appstate.MutationInfo{mutationInfo},
	}
	///////

	chat := ch.parent.GetChatClient()
	err := chat.SendAppState(patchInfo)
	if err != nil {
		fmt.Printf("Unable to mark conversation read: %s", err)
		return
	}

	ch.lock.Lock()
	// Actually mark the chats 'read' on the UI
	for _, messageRow := range ch.messageRows {
		if messageRow != nil && messageRow.Timestamp().Before(lastMessageTimestamp) {
			messageRow.message.Unread = false
		}
	}
	ch.lock.Unlock()

	// Update the UI
	fmt.Println("chatLoadedMarkRead: complete, updating ChatView with ResponseIgnore")
	ch.parent.QueueMessageWithIntent(ChatView, ch.chat, ResponseIgnore)
}

func (ch *ChatUiView) Update(msg *UiMessage) (Response, error) {
	fmt.Printf("ChatUiView.Update: Invoked: %v", msg)

	client := ch.parent.GetChatClient()
	if client == nil || !client.IsConnected() {
		return msg.Intent, nil
	}
	ch.newTaskWithTimeout(2 * time.Second)

	// Bind the event handler
	ch.lock.Lock()
	if ch.evtHandle != 0 {
		client.RemoveEventHandler(ch.evtHandle)
	}
	ch.evtHandle = client.AddEventHandler(ch.chatEventHandler)
	ch.lock.Unlock()

	var beforeTime time.Time

	switch t := msg.Payload.(type) {
	case *models.ConversationModel:
		ch.lock.Lock()
		ch.chat = t
		ch.composeText.SetText("")
		beforeTime = time.Now()
		ch.lock.Unlock()
		ch.ClearMessages(nil, nil)
		ch.closed = false

	case *models.MessageModel:
		if ch.chat != nil && ch.chat.ChatJID == t.ChatJID {
			if t.Timestamp.IsZero() {
				return ResponsePushView, fmt.Errorf("Invalid message timestamp in new message event")
			}

			ch.lock.Lock()
			defer ch.lock.Unlock()
			uiRow := NewMessageRowUi(ch.parent, t, ch.messageLoaded)

			if len(ch.messageRows) == 0 {
				ch.messageRows = append(ch.messageRows, uiRow)
				ch.messageList.Append(uiRow)
			} else if t.Timestamp.Before(ch.messageRows[0].Timestamp()) {
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

			return msg.Intent, nil
		}
	case *time.Time:
		// Set the period to load FROM
		beforeTime = *t
	case nil:
		if msg.Intent == ResponseBackView {
			ch.Close()
			return msg.Intent, nil
		} else {
			// Don't do anything, remain in the same chat
			fmt.Printf("ChatUiView.Update: remaining in chat view with intent: %v\n", msg.Intent)
		}

	default:
		return ResponseReplaceView, fmt.Errorf("Unable to handle message payload: %s", t)
	}

	if !client.IsLoggedIn() || ch.closed {
		fmt.Println("ChatUiView.Update: is closed or logged out")
		if ch.cancel != nil {
			ch.cancel()
		}
		return ResponseReplaceView, fmt.Errorf("Chat closed")
	}

	if !ch.closed {
		fmt.Printf("Loading initial chat messages in %s", ch.chat.Name)
		ch.statusRow.SetLoading("Loading messages...")
		// Fetch initial chat messages
		go ch.LoadMessages(nil, &beforeTime, 30)
	}

	return msg.Intent, nil
}

func (ch *ChatUiView) ClearMessages(startTimestamp *time.Time, endTimestamp *time.Time) {
	ch.lock.Lock()
	defer ch.messageList.Insert(ch.statusRow, 0)
	defer ch.lock.Unlock()

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
	ch.lock.RLock()
	defer ch.lock.RUnlock()

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

func (ch *ChatUiView) LoadOlderMessages(limit int) {
	oldest, _ := ch.getMessageTimestampRange()
	go ch.LoadMessages(nil, &oldest, limit)
}

func (ch *ChatUiView) LoadMessages(startTimestamp *time.Time, endTimestamp *time.Time, limit int) {
	fmt.Println("LoadMessages: %v, %v, %d", startTimestamp, endTimestamp, limit)
	if ch.closed {
		return
	}
	ch.newTaskWithTimeout(10 * time.Second)
	defer ch.cancel()

	client := ch.parent.GetChatClient()
	chatDB := ch.parent.GetChatDB()
	deviceJID := ch.parent.GetDeviceJID()
	chatJID := ch.chat.ChatJID
	if deviceJID == nil || chatDB == nil || client == nil {
		return
	}

	messages, err := chatDB.Message.GetBetween(ch.ctx, *deviceJID, chatJID, startTimestamp, endTimestamp, limit)
	if err != nil {
		ch.parent.QueueMessage(ErrorView, err)
		return
	}

	lenMessages := len(messages)
	lenOrig := len(ch.messageRows)
	fmt.Printf("Got %d messages in chat %s for device %s\n", lenMessages, chatJID, deviceJID)

	// Request more history
	if lenMessages == 0 && lenOrig > 0 {
		ch.loadMoreHistory()
		return
	} else if lenMessages == 0 && lenOrig == 0 {
		ch.statusRow.SetStatus("No messages")
		return
	}

	contacts, err := ch.parent.GetContacts()
	if err != nil {
		fmt.Printf("Unable to load contacts for messages, some messages may be missing sender details: %s", err.Error())
		contacts = make(map[types.JID]types.ContactInfo, 0)
	}
	pushNames, err := ch.parent.GetPushNames()
	if err != nil {
		fmt.Printf("Unable to load pushNames for messages, some messages may be missing sender details: %s", err.Error())
		pushNames = make(map[types.JID]*db.PushName, 0)
	}

	// Convert messages to models
	msgModels := make([]*models.MessageModel, lenMessages)
	for i, message := range messages {
		if message.Message == nil && message.MessageData != nil {
			err := message.LoadMessage()
			if err != nil {
				ch.parent.QueueMessage(ErrorView, fmt.Errorf("Unable to load message from message data: %v", message.MessageID))
				continue
			}
		}
		if !models.MessageShouldBeParsed(message.Message) {
			continue
		}
		if message.Timestamp().IsZero() {
			fmt.Printf("Found zero timestamp at %d: %v\n", lenMessages-i-1, message)
			continue
		}

		// Resolve member details
		senderMember := models.ResolveMember(client, contacts, pushNames, message.SenderJID)

		// convert to UI model
		model, err := models.GetMessageModel(client, senderMember.PushName, message)
		if err != nil {
			ch.parent.QueueMessage(ErrorView, fmt.Errorf("Unable to convert message event to message model: %v", err))
			continue
		}

		// Inverse the position (ascending order)
		msgModels[lenMessages-i-1] = model
	}

	// Insert messages into the existing messages (if any) to keep chronological order
	i := 0
	j := 0
	merged := make([]*messageRowUi, lenOrig+lenMessages)

	fmt.Printf("Locking for inserting messages\n")
	ch.lock.Lock()

	for i < lenOrig && j < lenMessages {
		if ch.messageRows[i] == nil {
			lenOrig--
			i++
		} else if msgModels[j] == nil || (ch.messageRows[i].Timestamp().Equal(msgModels[j].Timestamp) && ch.messageRows[i].message.ID == msgModels[j].ID) {
			lenMessages--
			j++
		} else if ch.messageRows[i].Timestamp().Before(msgModels[j].Timestamp) {
			merged[i+j] = ch.messageRows[i]
			i++
		} else {
			newRow := NewMessageRowUi(ch.parent, msgModels[j], ch.messageLoaded)
			merged[i+j] = newRow
			j++

			ch.messageList.Insert(newRow, i+j)
		}
	}
	for i < lenOrig {
		merged[i+j] = ch.messageRows[i]
		i++
	}
	for j < lenMessages {
		if msgModels[j] == nil {
			lenMessages--
			j++
			continue
		}
		newRow := NewMessageRowUi(ch.parent, msgModels[j], ch.messageLoaded)
		merged[i+j] = newRow
		j++

		ch.messageList.Insert(newRow, i+j)
	}
	ch.messageRows = merged[:lenOrig+lenMessages]

	// Mark any messages read if any are not already in the time range
	lastUnreadTimestamp := time.Time{}

	for i, messageRow := range ch.messageRows {
		if messageRow == nil {
			fmt.Printf("Message row %d is nil!\n", i)
			continue
		}
		if messageRow.message.Unread {
			if messageRow.Timestamp().After(lastUnreadTimestamp) {
				lastUnreadTimestamp = messageRow.Timestamp()
			}
		}
	}

	ch.statusRow.SetVisible(true)
	ch.statusRow.SetLoaded()

	ch.lock.Unlock()
	fmt.Printf("Unlocked after inserting messages\n")

	if !lastUnreadTimestamp.IsZero() {
		fmt.Printf("Marking chat %s read, last read until: %s\n", chatJID, lastUnreadTimestamp)
		go ch.chatLoadedMarkRead(chatJID, lastUnreadTimestamp, true)
	}

	fmt.Println("ChatUiView.LoadMessages completed")
}

func (ch *ChatUiView) getMessageTimestampRange() (time.Time, time.Time) {
	var newestTimestamp time.Time
	var oldestTimestamp time.Time
	now := time.Now()

	ch.lock.RLock()
	if len(ch.messageRows) > 0 {
		if ch.messageRows[0] != nil {
			oldestTimestamp = ch.messageRows[0].Timestamp()
		}
		i := 0
		for {
			if !oldestTimestamp.IsZero() || i >= len(ch.messageRows) {
				break
			}
			if ch.messageRows[i] != nil {
				oldestTimestamp = ch.messageRows[i].Timestamp()
			}
			i++
		}
		newestRow := ch.messageRows[len(ch.messageRows)-1]
		if newestRow != nil {
			newestTimestamp = newestRow.Timestamp()
		}
		if newestTimestamp.IsZero() {
			newestTimestamp = now
		}
	} else {
		newestTimestamp = now
		oldestTimestamp = time.Time{}
	}
	ch.lock.RUnlock()

	return oldestTimestamp, newestTimestamp
}

func (ch *ChatUiView) loadMoreHistory() {
	fmt.Println("loadMoreHistory")

ready:
	for {
		select {
		case <-ch.ctx.Done():
			return
		case <-ch.statusRow.ctx.Done():
			if ch.closed {
				return
			}
			break ready
		default:
			// Don't request if contexts are busy
			return
		}
	}

	fmt.Println("loadMoreHistory: Setting UI")
	ch.lock.Lock()
	ch.statusRow.ctx, ch.statusRow.cancel = context.WithTimeout(context.Background(), 10*time.Second)
	ch.statusRow.SetLoading("Requesting for more messages...")
	ch.lock.Unlock()

	feedback := make(chan RequestInfo, 1)
	go ch.parent.RequestHistory(ch.chat.ChatJID, 30, ch.statusRow.ctx, feedback)
	go ch.consumeRequestFeedback(ch.statusRow.ctx, ch.statusRow.cancel, feedback)
	fmt.Println("loadMoreHistory: Done")
}

func (ch *ChatUiView) consumeRequestFeedback(ctx context.Context, cancel context.CancelFunc, feedback chan RequestInfo) {
	defer close(feedback)
	defer cancel()

	var info RequestInfo = nil
ready:
	for {
		select {
		case infoReceved, ok := <-feedback:
			if ok {
				info = infoReceved
				break ready
			}
			// timeout
		case <-ctx.Done():
			break ready
		}
	}

	ch.lock.Lock()
	defer ch.lock.Unlock()

	if info == nil {
		fmt.Println("Timeout waiting for message request")
		// timeout
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		context.AfterFunc(ctx, func() {
			ch.statusRow.SetLoaded()
		})
		ch.statusRow.SetStatus("Timeout, no messages received")
	} else if info.Err() == nil {
		fmt.Printf("Message request returned success: %v\n", info)
		onDemandInfo, ok := info.(*models.OnDemandRequestInfo)
		if !ok {
			ch.statusRow.SetStatus("Expected request info to be of type OnDemandRequestInfo")
			return
		}

		minTime, maxTime := ch.getMessageTimestampRange()
		repeatable := onDemandInfo.MinTime != minTime || onDemandInfo.MinTime.After(maxTime)

		if repeatable {
			ch.statusRow.SetLoaded()
		} else {
			ch.statusRow.SetStatus("No more history")
		}
	} else {
		ch.statusRow.SetStatus(info.Err().Error())
	}
}

func (ch *ChatUiView) handleMessageListEdgeOvershot(posType gtk.PositionType) {
	if posType == gtk.PosTop {
		if ch.customScrolled && ch.customScrollMessage != nil {
			fmt.Printf("Already requested for newest history: top")
			return
		}
		ch.lock.Lock()
		ch.customScrolled = true
		ch.customScrollMessage = ch.messageRows[0]
		ch.lock.Unlock()

		// Load new messages in history from the oldest loaded message timestamp
		fmt.Printf("Got request to load older messages before oldest message\n")
		go ch.LoadOlderMessages(30)
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

	ch.newTaskWithTimeout(30 * time.Second)
	context.AfterFunc(ch.ctx, func() {
		ch.finishPendingMessage()
	})
	ch.lock.Lock()
	defer ch.lock.Unlock()

	ch.send.SetLabel("Sending...")
	ch.composeText.SetEditable(false)

	msgText := strings.Clone(ch.composeText.Text())
	fmt.Printf("Send event: %s\n", msgText)

	chatJID := ch.chat.ChatJID
	msgModel := models.NewPendingMessage("Pending", chatJID, *deviceJID, "Me", msgText, models.MessageTypeText, nil)
	pending := NewMessageRowUi(ch.parent, &msgModel, ch.messageLoaded)

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
		fmt.Printf("Error sending message: No pending message row\n")
		return
	}
	pending := ch.messagePending
	if pending.message != msg {
		fmt.Printf("Error sending message: Pending message row model does not match passed message\n")
		return
	}

	fmt.Println("Sending message...")
	message := waE2E.Message{
		Conversation: &msg.Message,
	}
	client := ch.parent.GetChatClient()
	sendReq, err := client.SendMessage(ctx, ch.chat.ChatJID, &message)
	if err == nil {
		deviceJID := ch.parent.GetDeviceJID()

		pending.message.ID = sendReq.ID
		pending.message.Timestamp = sendReq.Timestamp
		pending.message.ChatJID = msg.ChatJID
		pending.message.SenderJID = *deviceJID
		pending.message.RawMessage = &message

		dbMsg := msg.IntoDbMessage(deviceJID)
		if err := dbMsg.SaveMessageData(); err != nil {
			fmt.Printf("Error saving message data: %s", err.Error())
			return
		}

		// Update our chat database with the message
		chatDB := ch.parent.GetChatDB()
		if err := chatDB.Message.Put(ctx, *deviceJID, ch.chat.ChatJID, []*db.Message{&dbMsg}); err != nil {
			fmt.Printf("Error updating message database: %s", err.Error())
			return
		}

		// Update our conversation with the last updated
		err := chatDB.Conversation.UpdateLastMessageTimestamp(ctx, *deviceJID, ch.chat.ChatJID)
		if err != nil {
			fmt.Printf("Unable to update chat with last message time: %s", err.Error())
			return
		}

		// Have the chat list update recents
		ch.parent.QueueMessageWithIntent(ChatListView, nil, ResponseIgnore)

		fmt.Printf("Sent message\n")
	} else {
		fmt.Printf("Error sending message: %s\n", err.Error())
		return
	}
}
