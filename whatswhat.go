package main

import (
	"database/sql"
	"time"

	"github.com/rs/zerolog"
	"github.com/tiggilyboo/whatswhat/db"
	"github.com/tiggilyboo/whatswhat/view"

	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"

	_ "github.com/mattn/go-sqlite3"

	wwdb "github.com/tiggilyboo/whatswhat/db"
	"go.mau.fi/util/dbutil"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	wlog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

type ViewStack struct {
	lock    sync.Mutex
	history []view.Message
}

func NewViewStack() *ViewStack {
	return &ViewStack{
		lock:    sync.Mutex{},
		history: []view.Message{},
	}
}

func (s *ViewStack) Push(v view.Message) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.history = append(s.history, v)
}

func (s *ViewStack) Len() int {
	return len(s.history)
}

func (s *ViewStack) Pop() view.Message {
	s.lock.Lock()
	defer s.lock.Unlock()

	l := len(s.history)
	if l == 0 {
		return view.Undefined
	}

	v := s.history[l-1]
	s.history = s.history[:l-1]
	return v
}

func (s *ViewStack) Peek() view.Message {
	l := len(s.history)
	if l == 0 {
		return view.Undefined
	}

	return s.history[l-1]
}

type WhatsWhatApp struct {
	*gtk.Application
	window   *gtk.ApplicationWindow
	header   *gtk.HeaderBar
	back     *gtk.Button
	profile  *gtk.Button
	client   *whatsmeow.Client
	chatDB   *db.Database
	contacts map[types.JID]types.ContactInfo
	viewChan chan view.UiMessage
	ctx      context.Context

	ui struct {
		*gtk.Stack
		history *ViewStack
		members map[view.Message]view.UiView
		current view.UiView
	}
}

func NewWhatsWhatApp(ctx context.Context, app *gtk.Application) (*WhatsWhatApp, error) {
	ww := WhatsWhatApp{
		Application: app,
		ctx:         ctx,
	}

	ww.viewChan = make(chan view.UiMessage, 10)
	ww.ui.Stack = gtk.NewStack()
	ww.ui.Stack.SetName("WhatsWhatStack")

	ww.ui.history = NewViewStack()
	ww.ui.SetTransitionType(gtk.StackTransitionTypeSlideLeftRight)
	ww.ui.members = make(map[view.Message]view.UiView)

	ww.subscribeUiView(view.QrView, view.NewQrUiView(&ww))
	ww.subscribeUiView(view.ChatListView, view.NewChatListView(&ww))
	ww.subscribeUiView(view.ProfileView, view.NewProfileUiView(&ww))
	ww.subscribeUiView(view.ChatView, view.NewChatView(&ww))

	msgView := view.NewMessageView(&ww)
	ww.subscribeUiView(view.ErrorView, msgView)
	ww.subscribeUiView(view.LoadingView, msgView)

	ww.back = gtk.NewButtonFromIconName("go-previous-symbolic")
	ww.back.SetTooltipText("Back")
	ww.back.ConnectClicked(func() {
		last := ww.ui.history.Pop()
		current := ww.ui.history.Peek()
		for {
			if last == current {
				if ww.ui.history.Len() > 1 {
					current = ww.ui.history.Pop()
				} else {
					break
				}
			} else {
				break
			}
		}
		fmt.Printf("Clicked back from: %s to %s (%d left in the stack)", last, current, ww.ui.history.Len())
		ww.pushUiView(current)
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

func (ww *WhatsWhatApp) GetChatClient() *whatsmeow.Client {
	return ww.client
}

func (ww *WhatsWhatApp) GetChatDB() *db.Database {
	return ww.chatDB
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

func (ww *WhatsWhatApp) pushUiView(v view.Message) {
	fmt.Println("pushUiView: ", v)

	member, ok := ww.ui.members[v]
	if !ok {
		panic(fmt.Sprintf("Unknown UI view: %s", v))
	}

	// Wait while current member is busy
	current := ww.ui.current
	if current != nil {
		waitTimeout, cancel := context.WithDeadline(context.Background(), time.Now().Add(3*time.Second))
		defer cancel()

	ready:
		for {
			select {
			case <-current.Done():
				fmt.Print("Current UI view done")
				break ready
			case <-waitTimeout.Done():
				fmt.Print("Closing current UI view from timeout")
				current.Close()
				break ready
			}
		}
	}
	ww.ui.current = member

	// Loading is special, we pop it as well from history before pushing the next view
	if ww.ui.history.Peek() == view.LoadingView {
		ww.ui.history.Pop()
	}

	glib.IdleAdd(func() {
		ww.ui.SetVisibleChild(member)

		if ww.ui.history.Len() <= 1 {
			ww.back.SetVisible(false)
		} else {
			ww.back.SetVisible(true)
		}
		ww.ui.history.Push(v)
	})
}

func (ww *WhatsWhatApp) consumeMessages() {
	for msg := range ww.viewChan {
		glib.IdleAdd(func() {
			fmt.Println("consumeMessages: ", msg)
			member, ok := ww.ui.members[msg.Identifier]
			if !ok {
				fmt.Println("consumeMessages: UNHANDLED", msg)
			} else {
				fmt.Println("Updating view")
				err := member.Update(&msg)
				if err != nil {
					ww.QueueMessage(view.ErrorView, err)
					return
				}

				if ww.ui.history.Peek() != msg.Identifier {
					ww.pushUiView(msg.Identifier)
				}
			}
		})
	}
}

func (ww *WhatsWhatApp) QueueMessage(id view.Message, payload interface{}) {
	var msgErr error
	switch payload.(type) {
	case error:
		msgErr = payload.(error)
		payload = nil
	default:
		msgErr = nil
	}
	ww.viewChan <- view.UiMessage{
		Identifier: id,
		Payload:    payload,
		Error:      msgErr,
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
func (ww *WhatsWhatApp) handleMessage(evt *events.Message) {

	// TODO: Send notification on dbus

	deviceJID := ww.client.Store.ID
	existingChat, err := ww.chatDB.Conversation.Get(ww.ctx, evt.Info.Chat)
	if err != nil {
		ww.QueueMessage(view.ErrorView, err)
		return
	}
	existingChat.LastMessageTimestamp = evt.Info.Timestamp

	if err := ww.chatDB.Conversation.Put(ww.ctx, *deviceJID, existingChat); err != nil {
		ww.QueueMessage(view.ErrorView, err)
		return
	}
	//message :=
	messages := make([]*wwdb.HistorySyncMessageTuple, 1)
	marshaled, err := proto.Marshal(evt.Message)
	if err != nil {
		ww.QueueMessage(view.ErrorView, err)
		return
	}
	messages[0] = &wwdb.HistorySyncMessageTuple{
		Info:    &evt.Info,
		Message: marshaled,
	}
	if err := ww.chatDB.Message.Put(ww.ctx, *deviceJID, evt.Info.Chat, messages); err != nil {
		ww.QueueMessage(view.ErrorView, err)
		return
	}

	// TODO: Added to DB, now update the chat UI
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

	ctx, cancel := context.WithCancel(context.Background())
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

		messages := make([]*wwdb.HistorySyncMessageTuple, 0, len(convo.GetMessages()))
		for _, rawMsg := range convo.GetMessages() {
			msgEvt, err := ww.client.ParseWebMessage(chatJID, rawMsg.GetMessage())
			if err != nil {
				fmt.Println("Dropping historical message due to parse error in ", chatJID)
				continue
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

		if len(messages) > 0 {
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
			}
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
