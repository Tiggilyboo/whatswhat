package main

import (
	"database/sql"
	"time"

	"github.com/rs/zerolog"
	"github.com/tiggilyboo/whatswhat/db"
	"github.com/tiggilyboo/whatswhat/services"
	"github.com/tiggilyboo/whatswhat/view"
	"github.com/tiggilyboo/whatswhat/view/models"

	"context"
	"fmt"
	"os"
	"os/signal"

	_ "github.com/mattn/go-sqlite3"

	wwdb "github.com/tiggilyboo/whatswhat/db"
	"go.mau.fi/util/dbutil"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	wlog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

type WhatsWhatApp struct {
	*gtk.Application
	window   *gtk.ApplicationWindow
	header   *gtk.HeaderBar
	back     *gtk.Button
	profile  *gtk.Button
	overlay  *gtk.Overlay
	client   *whatsmeow.Client
	chatDB   *db.Database
	notifier *services.NotificationService
	contacts map[types.JID]types.ContactInfo
	viewChan chan view.UiMessage
	ctx      context.Context

	ui struct {
		*gtk.Stack
		history *view.ViewStack
		members map[view.Message]view.UiView
		current view.UiView
		overlay view.UiView
	}
}

func NewWhatsWhatApp(ctx context.Context, app *gtk.Application) (*WhatsWhatApp, error) {
	ww := WhatsWhatApp{
		Application: app,
		ctx:         ctx,
	}

	notifier, err := services.NewNotificationService(ctx)
	if err != nil {
		return nil, err
	}
	ww.notifier = notifier

	ww.viewChan = make(chan view.UiMessage, 10)
	ww.ui.Stack = gtk.NewStack()
	ww.ui.history = view.NewViewStack()
	ww.ui.SetTransitionType(gtk.StackTransitionTypeSlideLeftRight)
	ww.ui.members = make(map[view.Message]view.UiView)

	ww.subscribeUiView(view.QrView, view.NewQrUiView(&ww))
	ww.subscribeUiView(view.ChatListView, view.NewChatListView(&ww))
	ww.subscribeUiView(view.ProfileView, view.NewProfileUiView(&ww))
	ww.subscribeUiView(view.ChatView, view.NewChatView(&ww))

	msgView := view.NewMessageView(&ww)
	ww.subscribeUiView(view.ErrorView, msgView)
	ww.subscribeUiView(view.LoadingView, msgView)

	ww.overlay = gtk.NewOverlay()
	ww.overlay.SetHAlign(gtk.AlignCenter)
	ww.overlay.SetVAlign(gtk.AlignCenter)
	ww.overlay.SetVisible(false)

	ww.back = gtk.NewButtonFromIconName("go-previous-symbolic")
	ww.back.SetTooltipText("Back")
	ww.back.ConnectClicked(func() {
		current := ww.ui.history.Peek()
		ww.QueueMessageWithIntent(current, nil, view.ResponseBackView)
	})

	ww.profile = gtk.NewButtonFromIconName("avatar-default-symbolic")
	ww.profile.SetTooltipText("Account")
	ww.profile.SetVisible(false)
	ww.profile.ConnectClicked(func() {
		ww.QueueMessage(view.ProfileView, nil)
	})

	ww.header = gtk.NewHeaderBar()
	ww.header.PackStart(ww.back)
	ww.header.PackEnd(ww.profile)

	ww.window = gtk.NewApplicationWindow(app)
	ww.window.SetDefaultSize(800, 600)
	ww.window.SetChild(ww.ui)
	ww.window.SetTitle("WhatsWhat")
	ww.window.SetTitlebar(ww.header)

	ww.QueueMessage(view.ChatListView, nil)
	ww.back.SetVisible(false)

	return &ww, nil
}

func (ww *WhatsWhatApp) QueueMessageWithIntent(id view.Message, payload interface{}, intent view.Response) {
	ww.viewChan <- view.UiMessage{
		Identifier: id,
		Payload:    payload,
		Intent:     intent,
	}
}

func (ww *WhatsWhatApp) QueueOverlayMessage(id view.Message, payload interface{}) {
	ww.QueueMessageWithIntent(id, payload, view.ResponseOverlay)
}

func (ww *WhatsWhatApp) QueueMessage(id view.Message, payload interface{}) {
	ww.QueueMessageWithIntent(id, payload, view.ResponsePushView)
}

func (ww *WhatsWhatApp) GetChatClient() *whatsmeow.Client {
	return ww.client
}

func (ww *WhatsWhatApp) GetChatDB() *db.Database {
	return ww.chatDB
}

func (ww *WhatsWhatApp) GetDeviceJID() *types.JID {
	if ww.client == nil || ww.client.Store.ID == nil {
		return nil
	}
	return ww.client.Store.ID
}

func (ww *WhatsWhatApp) GetWindowSize() (int, int) {
	width := ww.window.Size(gtk.OrientationHorizontal)
	height := ww.window.Size(gtk.OrientationVertical)
	return width, height
}

func (ww *WhatsWhatApp) GetContacts() (map[types.JID]types.ContactInfo, error) {
	if ww.contacts != nil {
		return ww.contacts, nil
	}

	contacts, err := ww.client.Store.Contacts.GetAllContacts()
	if err != nil {
		return nil, err
	}

	return contacts, nil
}

func (ww *WhatsWhatApp) subscribeUiView(ident view.Message, ui view.UiView) {
	if _, exists := ww.ui.members[ident]; exists {
		panic(fmt.Sprint("Already subscribed UI: ", ident))
	}
	ww.ui.members[ident] = ui
	ww.ui.AddChild(ui)
}

func (ww *WhatsWhatApp) getUiView(v view.Message) view.UiView {
	member, ok := ww.ui.members[v]
	if !ok {
		return nil
	}
	return member
}

func (ww *WhatsWhatApp) waitViewDone(v view.UiView) {
	// Wait while current member is busy
	if v == nil {
		return
	}
	waitTimeout, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

ready:
	for {
		select {
		case <-v.Done():
			fmt.Printf("UI view done\n")
			break ready
		case <-waitTimeout.Done():
			fmt.Print("Closing current UI view from timeout")
			v.Close()
			break ready
		}
	}
}

func (ww *WhatsWhatApp) updateOverlayUiView(v view.Message, member view.UiView, waitDone bool) {
	// Wait for any other overlays to complete
	ww.waitViewDone(ww.ui.overlay)

	ww.overlay.SetChild(member)

	ww.ui.overlay = member
}

func (ww *WhatsWhatApp) updateCurrentUiView(v view.Message, member view.UiView, changeVisibleChild bool, pushHistory bool, waitDone bool) {
	if waitDone {
		//ww.waitViewDone(ww.ui.overlay)
		ww.waitViewDone(ww.ui.current)
	}

	// Loading and message views are special, we pop them as well from history before pushing the next view
	peeked := ww.ui.history.Peek()
	for {
		if peeked == view.ErrorView {
			peeked = ww.ui.history.Pop()
		} else {
			break
		}
	}

	ww.ui.current = member
	if pushHistory {
		ww.ui.history.Push(v)
	}

	glib.IdleAdd(func() {
		ww.window.SetTitle(member.Title())

		if ww.ui.history.Len() <= 1 {
			ww.back.SetVisible(false)
		} else {
			ww.back.SetVisible(true)
		}
		if changeVisibleChild {
			ww.ui.SetVisibleChild(member)
		}
	})
}

func (ww *WhatsWhatApp) consumeMessages() {
	for msg := range ww.viewChan {
		fmt.Println("consumeMessages: ", msg)

		member, ok := ww.ui.members[msg.Identifier]
		if !ok {
			fmt.Println("consumeMessages: UNHANDLED", msg)
		} else {
			glib.IdleAdd(func() {
				fmt.Println("Updating view")
				response, err := member.Update(&msg)
				if err != nil {
					ww.QueueMessage(view.ErrorView, err)
					return
				}
				switch response {
				case view.ResponseIgnore:
					// Don't do anything with view stack
					ww.updateCurrentUiView(msg.Identifier, member, false, false, true)

				case view.ResponseBackView:
					last := ww.ui.history.Pop()
					current := ww.ui.history.Peek()
					if current == view.Undefined {
						current = view.ChatListView
					}
					member = ww.getUiView(current)
					ww.updateCurrentUiView(current, member, true, false, true)

					fmt.Printf("Went back from: %s to %s (%d left in the stack)\n", last, current, ww.ui.history.Len())

				case view.ResponseReplaceView:
					last := ww.ui.history.Pop()
					member = ww.getUiView(msg.Identifier)
					ww.updateCurrentUiView(msg.Identifier, member, true, true, true)
					fmt.Printf("Replaced view from %v to %v", last, msg.Identifier)

				case view.ResponsePushView:
					if ww.ui.history.Len() == 0 || ww.ui.history.Peek() != msg.Identifier {
						member = ww.getUiView(msg.Identifier)
						ww.updateCurrentUiView(msg.Identifier, member, true, true, true)
					} else {
						ww.updateCurrentUiView(msg.Identifier, member, true, false, true)
					}

				case view.ResponseOverlay:
					member = ww.getUiView(msg.Identifier)
					ww.updateOverlayUiView(msg.Identifier, member, true)
				}
			})
		}
	}
}

func (ww *WhatsWhatApp) handleConnectedState(connected bool) {
	if ww.client.IsLoggedIn() {
		contacts, err := ww.client.Store.Contacts.GetAllContacts()
		if err != nil {
			ww.QueueMessage(view.ErrorView, err)
			return
		}
		fmt.Println("loaded ", len(contacts), " contacts")
		ww.contacts = contacts

		ww.profile.SetVisible(true)
	} else {
		ww.profile.SetVisible(false)
	}
}

func (ww *WhatsWhatApp) queueMessageNotification(id uint32, evt *events.Message) {
	msgModel, err := models.GetMessageModel(ww.client, evt.Info.Chat, evt)
	if err != nil {
		ww.QueueMessage(view.ErrorView, err)
		return
	}
	notification := services.Notification{
		ID:            id,
		Summary:       msgModel.PushName,
		Body:          msgModel.Message,
		Icon:          "mail-unread",
		ExpirySeconds: 0,
	}
	ww.notifier.QueueNotification(&notification)
}

func (ww *WhatsWhatApp) queueUnreadChatNotification(id uint32, convo *waHistorySync.Conversation) {
	unreadCount := uint(convo.GetUnreadCount())
	if unreadCount == 0 {
		return
	}
	ts := time.Time{}
	if convo.LastMsgTimestamp != nil && *convo.LastMsgTimestamp > 0 {
		ts = time.Unix(int64(*convo.LastMsgTimestamp), 0)
	}
	chatJID, err := types.ParseJID(*convo.ID)
	if err != nil {
		return
	}
	chatName := ""
	if convo.Name != nil {
		chatName = *convo.Name
	}
	convoModel, err := models.GetConversationModel(ww.client, ww.contacts, chatJID, chatName, unreadCount, ts, false)
	if err != nil {
		ww.QueueMessage(view.ErrorView, err)
		return
	}

	notification := services.Notification{
		ID:            id,
		Summary:       convoModel.Name,
		Body:          fmt.Sprintf("%d unread messages", convoModel.UnreadCount),
		Icon:          "mail-unread",
		ExpirySeconds: 0,
	}
	ww.notifier.QueueNotification(&notification)
}

func (ww *WhatsWhatApp) handleMessage(evt *events.Message) {
	deviceJID := ww.client.Store.ID
	existingChat, err := ww.chatDB.Conversation.Get(ww.ctx, *deviceJID, evt.Info.Chat)
	if err != nil {
		ww.QueueMessage(view.ErrorView, fmt.Errorf("No existing chat: %s", err.Error()))
		return
	}
	// Create a new conversation for this message
	if existingChat == nil {
		newConversation := db.Conversation{
			DeviceJID: *deviceJID,
			ChatJID:   evt.Info.Chat,
			Name:      evt.Info.PushName,
		}
		existingChat = &newConversation
	}

	// Update the last message time to latest received event
	existingChat.LastMessageTimestamp = evt.Info.Timestamp

	// TODO: Notification ID from conversation ID
	go ww.queueMessageNotification(0, evt)

	if err := ww.chatDB.Conversation.Put(ww.ctx, *deviceJID, existingChat); err != nil {
		ww.QueueMessage(view.ErrorView, fmt.Errorf("Unable to Put conversation for new message: %s", err.Error()))
		return
	}
	//message :=
	messages := make([]*wwdb.HistorySyncMessageTuple, 1)

	fmt.Printf("Message: %v", evt.UnwrapRaw())

	marshaled, err := proto.Marshal(evt.Message)
	if err != nil {
		ww.QueueMessage(view.ErrorView, fmt.Errorf("Unable to marshal new message: %s", err.Error()))
		return
	}
	messages[0] = &wwdb.HistorySyncMessageTuple{
		Info:    &evt.Info,
		Message: marshaled,
	}
	fmt.Println("Updating message in chatDB: ", messages[0].GetMassInsertValues())
	if err := ww.chatDB.Message.Put(ww.ctx, *deviceJID, evt.Info.Chat, messages); err != nil {
		ww.QueueMessage(view.ErrorView, err)
		return
	}
	if err := ww.chatDB.Conversation.UpdateLastMessageTimestamp(ww.ctx, *deviceJID, evt.Info.Chat); err != nil {
		ww.QueueMessage(view.ErrorView, err)
		return
	}

	// TODO: Added to DB, now update the chat UI
}

func (ww *WhatsWhatApp) handleLoggedOut(evt *events.LoggedOut) {

	ww.ui.history.Clear()
	ww.QueueMessage(view.ChatListView, nil)

	deviceJID := ww.GetDeviceJID()

	// Try to connect again?
	if evt.OnConnect && !ww.client.IsConnected() {
		err := ww.client.Connect()
		if err != nil {
			ww.QueueMessage(view.QrView, fmt.Errorf("Unable to reconnect: %s", err.Error()))
			return
		}
	}

	// Delete session data?
	if evt.Reason.IsLoggedOut() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		go ww.chatDB.Conversation.DeleteAll(ctx, *deviceJID)
	}

	ww.QueueMessage(view.LoadingView, evt.Reason.String())
}

func (ww *WhatsWhatApp) handleHistorySync(evt *events.HistorySync) {
	if ww.chatDB == nil {
		ww.QueueMessage(view.ErrorView, fmt.Errorf("Chat database not initialized, cannot load chat history"))
		return
	}
	if ww.client == nil || !ww.client.IsLoggedIn() {
		ww.QueueMessage(view.ErrorView, fmt.Errorf("Unable to handle history sync, logged in!"))
		return
	}
	ww.QueueMessage(view.LoadingView, "Loading chat history...")

	ctx, cancel := context.WithCancel(context.Background())
	context.AfterFunc(ctx, func() {
		ww.QueueMessageWithIntent(view.ChatListView, nil, view.ResponseReplaceView)
	})
	defer cancel()

	failedToSaveConversations := 0
	failedToSaveMessages := 0
	addedMessages := 0

	deviceID := ww.client.Store.ID
	for _, convo := range evt.Data.Conversations {
		chatJID, err := types.ParseJID(convo.GetID())
		if err != nil {
			ww.QueueMessage(view.ErrorView, err)
			return
		}

		var maxTime time.Time
		messages := make([]*wwdb.HistorySyncMessageTuple, 0, len(convo.GetMessages()))
		for _, rawMsg := range convo.GetMessages() {
			msgEvt, err := ww.client.ParseWebMessage(chatJID, rawMsg.GetMessage())
			if err != nil {
				fmt.Println("Dropping historical message due to parse error in ", chatJID)
				continue
			}
			if maxTime.IsZero() || msgEvt.Info.Timestamp.After(maxTime) {
				maxTime = msgEvt.Info.Timestamp
			}

			marshaled, err := proto.Marshal(rawMsg)
			if err != nil {
				fmt.Println("Dropping historical message due to marshal error in ", chatJID)
				continue
			}

			tuple := &wwdb.HistorySyncMessageTuple{
				Info:    &msgEvt.Info,
				Message: marshaled,
			}
			messages = append(messages, tuple)
		}

		// Update the last message time
		var convoLastMsgTime time.Time
		if convo.LastMsgTimestamp == nil || *convo.LastMsgTimestamp == 0 {
			convoLastMsgTime = time.Time{}
		} else {
			convoLastMsgTime = time.Unix(int64(*convo.LastMsgTimestamp), 0)
		}
		if maxTime.After(convoLastMsgTime) {
			convoLastMsgTime = maxTime
		}
		lastMsgUnixTs := uint64(convoLastMsgTime.Unix())
		convo.LastMsgTimestamp = &lastMsgUnixTs

		if len(messages) > 0 {
			// Convo timestamp is the last message
			dbConvo := wwdb.NewConversation(*deviceID, chatJID, convo)
			if err := ww.chatDB.Conversation.Put(ctx, *deviceID, dbConvo); err != nil {
				failedToSaveConversations += 1
				fmt.Printf("Unable to save conversation metadata: %s\n", err)
				continue
			}
			if err := ww.chatDB.Message.Put(ctx, *deviceID, chatJID, messages); err != nil {
				failedToSaveMessages += len(messages)
				fmt.Printf("Unable to save messages: %s\n", err)
				continue
			} else {
				addedMessages += len(messages)

				if err := ww.chatDB.Conversation.UpdateLastMessageTimestamp(ctx, *deviceID, dbConvo.ChatJID); err != nil {
					failedToSaveConversations += 1
					fmt.Printf("Unable to update last message timestamp in conversation metadata: %s\n", err)
					continue
				}
			}
		}

		// Received some unread messages, send notifications
		if len(messages) > 0 && convo.GetUnreadCount() > 0 {
			// TODO: Notification ID from conversation ID
			go ww.queueUnreadChatNotification(0, convo)
		}
	}

	if failedToSaveConversations > 0 || failedToSaveMessages > 0 {
		ww.QueueMessage(view.ErrorView, fmt.Errorf("Failed to save %s conversations and %s messages", failedToSaveConversations, failedToSaveMessages))
		return
	} else {
		fmt.Printf("Added %s messages from history sync", addedMessages)
	}
}

func (ww *WhatsWhatApp) handleCommonEvents(evt interface{}) {
	switch v := evt.(type) {
	case *events.Connected:
		ww.handleConnectedState(true)
	case *events.Disconnected:
		ww.handleConnectedState(false)
	case *events.HistorySync:
		ww.handleHistorySync(v)
	case *events.Message:
		ww.handleMessage(v)
	case *events.LoggedOut:
		ww.handleLoggedOut(v)
	}
}

func (ww *WhatsWhatApp) Initialize(ctx context.Context) error {
	sqlDb, err := sql.Open("sqlite3", "file:whatswhat.db?_foreign_keys=on")
	if err != nil {
		return err
	}

	dbLog := wlog.Stdout("Database", "DEBUG", true)
	container := sqlstore.NewWithDB(sqlDb, "sqlite3", dbLog)

	fmt.Println("Upgrading whatsapp database")
	if err := container.Upgrade(); err != nil {
		return err
	}

	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		return err
	}
	clientLog := wlog.Stdout("Client", "DEBUG", true)
	wrappedDb, err := dbutil.NewWithDB(sqlDb, "sqlite3")
	if err != nil {
		return err
	}

	chatDbLog := zerolog.New(os.Stdout)
	chatDb := wwdb.New(wrappedDb, chatDbLog)
	ww.chatDB = chatDb

	fmt.Println("Upgrading chat database")
	if err := chatDb.Upgrade(ctx); err != nil {
		return err
	}

	fmt.Println("Creating new whatsapp client")
	ww.client = whatsmeow.NewClient(deviceStore, clientLog)
	ww.client.AddEventHandler(ww.handleCommonEvents)

	ww.client.EnableAutoReconnect = true
	ww.client.AutoTrustIdentity = true

	// New login?
	if ww.client.Store.ID == nil {
		// initially set QR view without code
		ww.QueueMessage(view.QrView, nil)
		ww.profile.SetVisible(false)
	} else {
		// Already logged in, connect
		if err = ww.client.Connect(); err != nil {
			return err
		}
		ww.profile.SetVisible(true)
		ww.QueueMessage(view.ChatListView, nil)
	}

	return nil
}

func Run() {
	// Initialize UI
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	app := gtk.NewApplication("com.github.tiggilyboo.whatswhat", gio.ApplicationFlagsNone)
	app.ConnectActivate(func() {
		ww, err := NewWhatsWhatApp(ctx, app)
		if err != nil {
			ww.QueueMessage(view.ErrorView, err)
		} else {
			go ww.consumeMessages()

			if err == nil {
				err = ww.Initialize(ctx)
				if err != nil {
					ww.QueueMessage(view.ErrorView, err)
				}
			}
		}

		ww.window.SetVisible(true)
	})
	go func() {
		<-ctx.Done()
		glib.IdleAdd(app.Quit)
	}()
	if code := app.Run(os.Args); code > 0 {
		cancel()
		os.Exit(code)
	}
}

func main() {
	Run()
}
