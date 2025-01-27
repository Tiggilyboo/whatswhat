package main

import (
	"time"

	"github.com/tiggilyboo/whatswhat/view"

	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"

	_ "github.com/mattn/go-sqlite3"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	wlog "go.mau.fi/whatsmeow/util/log"

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
	ww.subscribeUiView(view.ChatView, view.NewChatView(&ww))
	ww.subscribeUiView(view.ProfileView, view.NewProfileUiView(&ww))

	msgView := view.NewMessageView(&ww)
	ww.subscribeUiView(view.ErrorView, msgView)
	ww.subscribeUiView(view.LoadingView, msgView)

	ww.back = gtk.NewButtonFromIconName("go-previous-symbolic")
	ww.back.SetTooltipText("Back")
	ww.back.ConnectClicked(func() {
		last := ww.ui.history.Pop()
		fmt.Println("Clicked back from: ", last)

		current := ww.ui.history.Peek()
		ww.pushUiView(current)
	})

	ww.profile = gtk.NewButtonFromIconName("avatar-default-symbolic")
	ww.profile.SetTooltipText("Account")
	ww.profile.SetVisible(false)
	ww.profile.ConnectClicked(func() {
		fmt.Println("Clicked profile")
		ww.pushUiView(view.ProfileView)
	})

	ww.header = gtk.NewHeaderBar()
	ww.header.PackStart(ww.back)
	ww.header.PackEnd(ww.profile)

	ww.window = gtk.NewApplicationWindow(app)
	ww.window.SetDefaultSize(800, 600)
	ww.window.SetChild(ww.ui)
	ww.window.SetTitle("WhatsWhat")
	ww.window.SetTitlebar(ww.header)

	ww.pushUiView(view.ChatView)
	ww.back.SetVisible(false)

	return &ww, nil
}

func (ww *WhatsWhatApp) GetChatClient() *whatsmeow.Client {
	return ww.client
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

	// Wait until current member is busy
	current := ww.ui.current
	if current != nil {
		waitTimeout, _ := context.WithTimeout(context.Background(), 1*time.Second)

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

	ww.ui.SetVisibleChild(member)
	ww.ui.current = member

	ww.ui.history.Push(v)
	if ww.ui.history.Len() <= 1 {
		ww.back.SetVisible(false)
	} else {
		ww.back.SetVisible(true)
	}
}

func (ww *WhatsWhatApp) consumeMessages() {
	for msg := range ww.viewChan {
		glib.IdleAdd(func() {
			fmt.Println("consumeMessages: ", msg)
			member, ok := ww.ui.members[msg.Identifier]
			if !ok {
				fmt.Println("consumeMessages: UNHANDLED", msg)
			} else {
				member.Update(&msg)

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
		ww.profile.SetVisible(true)
	} else {
		ww.profile.SetVisible(false)
	}
}

func (ww *WhatsWhatApp) handleCommonEvents(evt interface{}) {
	switch evt.(type) {
	case *events.Connected:
		ww.handleConnectedState(true)
	case *events.Disconnected:
		ww.handleConnectedState(false)
	}
}

func (ww *WhatsWhatApp) InitializeChat(ctx context.Context) {
	dbLog := wlog.Stdout("Database", "DEBUG", true)
	container, err := sqlstore.New("sqlite3", "file:whatswhat.db?_foreign_keys=on", dbLog)
	if err != nil {
		ww.QueueMessage(view.ErrorView, err)
		return
	}

	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		ww.QueueMessage(view.ErrorView, err)
		return
	}

	clientLog := wlog.Stdout("Client", "DEBUG", true)
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
			ww.QueueMessage(view.ErrorView, err.Error())
			return
		}
		ww.profile.SetVisible(true)
		ww.QueueMessage(view.ChatView, nil)
	}
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
		}
		ww.window.SetVisible(true)
		go ww.consumeMessages()

		go ww.InitializeChat(ctx)
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
