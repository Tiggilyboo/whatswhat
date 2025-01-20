package whatswhat

import (
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

func eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		fmt.Println("Received a message!", v.Message.GetConversation())
	}
}

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
	client   *whatsmeow.Client
	viewChan chan view.UiMessage
	ctx      context.Context

	ui struct {
		*gtk.Stack
		history *ViewStack
		members map[view.Message]view.UiView
	}
}

func NewWhatsWhatApp(ctx context.Context, app *gtk.Application) (*WhatsWhatApp, error) {
	ww := WhatsWhatApp{
		Application: app,
		ctx:         ctx,
	}

	ww.viewChan = make(chan view.UiMessage, 10)
	ww.ui.Stack = gtk.NewStack()
	ww.ui.history = NewViewStack()
	ww.ui.SetTransitionType(gtk.StackTransitionTypeSlideLeftRight)
	ww.ui.members = make(map[view.Message]view.UiView)

	ww.subscribeUiView(view.QrView, view.NewQrUiView(&ww))
	ww.subscribeUiView(view.ChatView, view.NewChatView(&ww))

	msgView := view.NewMessageView(&ww)
	ww.subscribeUiView(view.ErrorView, msgView)
	ww.subscribeUiView(view.LoadingView, msgView)

	ww.window = gtk.NewApplicationWindow(app)
	ww.window.SetDefaultSize(800, 600)
	ww.window.SetChild(ww.ui)
	ww.window.SetTitle("WhatsWhat")
	ww.window.SetTitlebar(ww.header)

	ww.back = gtk.NewButtonFromIconName("go-previous-symbolic")
	ww.back.SetTooltipText("Back")
	ww.back.ConnectClicked(func() {
		ww.ui.history.Pop()
		ww.pushUiView(ww.ui.history.Peek())
	})
	ww.pushUiView(view.ChatView)
	ww.ui.history.Pop()
	ww.back.SetVisible(false)

	ww.header = gtk.NewHeaderBar()
	ww.header.PackStart(ww.back)

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
	member, ok := ww.ui.members[v]
	if !ok {
		panic(fmt.Sprintf("Unknown UI view: %s", v))
	}

	ww.ui.SetVisibleChild(member)
	ww.ui.history.Push(v)
	if ww.ui.history.Len() > 1 {
		ww.back.SetVisible(true)
	} else {
		ww.back.SetVisible(false)
	}
}

func (ww *WhatsWhatApp) consumeMessages() {
	for msg := range ww.viewChan {
		glib.IdleAdd(func() {
			fmt.Println("consumeMessages: ", msg)
			member, ok := ww.ui.members[msg.View]
			if !ok {
				fmt.Println("consumeMessages: UNHANDLED", msg)
			} else {
				member.Update(&msg)
			}
		})
	}
}

func (ww *QrUiView) consumeQrMessages() {
	for {
		select {
		case evt, ok := <-ww.ui.qr.qrChan:
			if !ok {
				// channel closed, exit
				fmt.Println("QR channel closed, exiting")
				return
			}
			fmt.Println("QR channel received: ", evt)

			if evt.Error != nil {
				ww.QueueMessage(view.QrView, evt.Error)
				break
			}
			if evt.Event == "code" {
				ww.QueueMessage(view.QrView, evt.Code)
			} else {
				fmt.Println("Login event: ", evt.Event)
			}
		}
	}
}

func (ww *WhatsWhatApp) ViewQrCode(msg *view.ViewMessage) {
	fmt.Println("ViewQrCode: ", msg)

	// Remove old QR image
	if ww.ui.qr.qrImage != nil {
		ww.ui.qr.view.Remove(ww.ui.qr.qrImage)
	}

	// No code, reset and make new QR chan
	if msg.Error != nil && msg.Payload == nil {
		if ww.ui.qr.reset != nil {
			ww.ui.qr.reset()
			ww.ui.qr.ctx, ww.ui.qr.reset = context.WithCancel(context.Background())
		}

		var err error
		ww.ui.qr.qrChan, err = ww.client.GetQRChannel(ww.ui.qr.ctx)
		if err != nil {
			ww.QueueMessage(view.ErrorView, err)
			return
		}

		if err = ww.client.Connect(); err != nil {
			ww.QueueMessage(view.ErrorView, err)
			return
		}

		// Consume any new QR messages in another routine
		go ww.consumeQrMessages()
	}

	ww.ui.ShowUiView(view.QrView)
}

func (ww *WhatsWhatApp) QueueMessage(view view.View, payload interface{}) {
	var msgErr error
	switch payload.(type) {
	case error:
		msgErr = payload.(error)
		payload = nil
	default:
		msgErr = nil
	}
	ww.viewChan <- view.UiMessage{
		View:    view,
		Payload: payload,
		Error:   msgErr,
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
	ww.client.AddEventHandler(eventHandler)

	// New login?
	if ww.client.Store.ID == nil {
		// initially set QR view without code
		ww.QueueMessage(view.QrView, nil)
	} else {
		// Already logged in, connect
		if err = ww.client.Connect(); err != nil {
			ww.QueueMessage(view.ErrorView, err.Error())
			return
		}

		ww.QueueMessage(view.ChatView, nil)
	}
}

func main() {
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
