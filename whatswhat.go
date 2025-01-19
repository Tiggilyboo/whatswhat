package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	_ "github.com/mattn/go-sqlite3"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	wlog "go.mau.fi/whatsmeow/util/log"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/skip2/go-qrcode"
)

type View uint8

const (
	LoadingView View = iota
	QrView
	ChatView
	ErrorView
)

func eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		fmt.Println("Received a message!", v.Message.GetConversation())
	}
}

type chatView struct {
	*gtk.ScrolledWindow
	parent *whatsWhat
	view   *gtk.Box
	login  *gtk.Button

	ctx    context.Context
	cancel context.CancelFunc
}

type qrView struct {
	*gtk.ScrolledWindow
	parent      *whatsWhat
	view        *gtk.Box
	qrImage     *gtk.Image
	qrChan      <-chan whatsmeow.QRChannelItem
	description *gtk.Label
	retry       *gtk.Button

	ctx    context.Context
	cancel context.CancelFunc
}

type ViewMessage struct {
	View    View
	Payload interface{}
	Error   error
}

type whatsWhat struct {
	*gtk.Application
	window   *gtk.ApplicationWindow
	header   *gtk.HeaderBar
	back     *gtk.Button
	client   *whatsmeow.Client
	viewChan chan ViewMessage
	ctx      context.Context

	view struct {
		*gtk.Stack
		current View
		chats   *chatView
		qr      *qrView
	}
}

func newChatView(ww *whatsWhat) *chatView {
	ctx, cancel := context.WithCancel(context.Background())
	v := chatView{
		ctx:    ctx,
		cancel: cancel,
		parent: ww,
	}
	v.view = gtk.NewBox(gtk.OrientationVertical, 0)

	v.login = gtk.NewButtonWithLabel("Login")
	v.login.ConnectClicked(func() {
		ww.queueViewMessage(QrView, nil)
	})
	v.view.Append(v.login)

	viewport := gtk.NewViewport(nil, nil)
	viewport.SetScrollToFocus(true)
	viewport.SetChild(v.view)

	v.ScrolledWindow = gtk.NewScrolledWindow()
	v.ScrolledWindow.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	v.ScrolledWindow.SetChild(viewport)
	v.ScrolledWindow.SetPropagateNaturalHeight(true)

	return &v
}

func newQrView(ww *whatsWhat) *qrView {
	ctx, cancel := context.WithCancel(context.Background())
	v := qrView{
		ctx:    ctx,
		cancel: cancel,
		parent: ww,
	}
	v.qrImage = gtk.NewImageFromIconName("image-missing-symbolic")
	v.qrImage.SetIconSize(gtk.IconSizeLarge)
	v.view = gtk.NewBox(gtk.OrientationVertical, 5)
	v.view.Append(v.qrImage)

	v.description = gtk.NewLabel("Loading...")
	v.description.SetVExpand(true)
	v.description.SetVExpand(true)
	v.view.Append(v.description)

	v.retry = gtk.NewButtonFromIconName("update-symbolic")
	v.retry.ConnectClicked(func() {
		ww.queueViewMessage(QrView, nil)
	})
	v.view.Append(v.retry)

	viewport := gtk.NewViewport(nil, nil)
	viewport.SetScrollToFocus(true)
	viewport.SetChild(v.view)

	v.ScrolledWindow = gtk.NewScrolledWindow()
	v.ScrolledWindow.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	v.ScrolledWindow.SetChild(viewport)
	v.ScrolledWindow.SetPropagateNaturalHeight(true)

	return &v
}

func newWhatsWhat(ctx context.Context, app *gtk.Application) (*whatsWhat, error) {
	ww := whatsWhat{
		Application: app,
		ctx:         ctx,
	}

	ww.viewChan = make(chan ViewMessage, 10)
	ww.view.Stack = gtk.NewStack()
	ww.view.SetTransitionType(gtk.StackTransitionTypeSlideLeftRight)

	ww.view.qr = newQrView(&ww)
	ww.view.chats = newChatView(&ww)

	ww.view.AddChild(ww.view.chats)
	ww.view.AddChild(ww.view.qr)

	ww.view.current = LoadingView

	loadingBox := gtk.NewBox(gtk.OrientationVertical, 0)
	loading := gtk.NewLabel("Loading database...")
	loadingBox.Append(loading)
	ww.view.SetVisibleChild(loadingBox)

	ww.back = gtk.NewButtonFromIconName("go-previous-symbolic")
	ww.back.SetVisible(false)
	ww.back.SetTooltipText("Back")
	ww.back.ConnectClicked(ww.ViewChats)

	ww.header = gtk.NewHeaderBar()
	ww.header.PackStart(ww.back)

	ww.window = gtk.NewApplicationWindow(app)
	ww.window.SetDefaultSize(800, 600)
	ww.window.SetChild(ww.view)
	ww.window.SetTitle("WhatsWhat")
	ww.window.SetTitlebar(ww.header)

	return &ww, nil
}

func (ww *whatsWhat) consumeMessages() {
	for msg := range ww.viewChan {
		glib.IdleAdd(func() {
			fmt.Println("consumeMessages: ", msg)
			switch msg.View {
			case ErrorView:
				ww.ViewError(&msg)
			case QrView:
				ww.ViewQrCode(&msg)
			case ChatView:
				ww.ViewChats()
			}
		})
	}
}

func (ww *whatsWhat) consumeQrMessages() {
	for {
		select {
		case <-ww.view.qr.ctx.Done():
			if ww.view.qr.ctx.Err() != nil {
				fmt.Println("QR context done, resetting channel")
				// cancelled, reset context for future
				ww.view.qr.ctx, ww.view.qr.cancel = context.WithCancel(context.Background())
				ww.view.qr.qrChan = nil
				return
			}

		case evt, ok := <-ww.view.qr.qrChan:
			if !ok {
				// channel closed, exit
				fmt.Println("QR channel closed, exiting")
				return
			}
			fmt.Println("QR channel received: ", evt)

			if evt.Error != nil {
				ww.queueViewMessage(QrView, evt.Error)
				break
			}
			if evt.Event == "code" {
				ww.queueViewMessage(QrView, evt.Code)
			} else {
				fmt.Println("Login event: ", evt.Event)
			}
		}
	}
}

func (ww *whatsWhat) ViewChats() {
	fmt.Println("ViewChats: Invoked")

	ww.window.SetTitle("WhatsWhat - Chat")
	ww.view.SetVisibleChild(ww.view.chats)
	ww.back.SetVisible(false)
	ww.view.current = ChatView

	if ww.client == nil || !ww.client.IsLoggedIn() {
		ww.view.chats.login.SetVisible(true)
		return
	}

	// Client is logged in!
	fmt.Println("Logged in")
	ww.view.chats.login.SetVisible(false)
}

func (ww *whatsWhat) ViewError(msg *ViewMessage) {
	fmt.Println("ViewError: ", msg)

	messageBox := gtk.NewBox(gtk.OrientationVertical, 0)

	var text *gtk.Label
	if msg.Error != nil {
		text = gtk.NewLabel(msg.Error.Error())
	} else {
		text = gtk.NewLabel(fmt.Sprint(msg.Payload))
	}
	messageBox.Append(text)

	ww.window.SetTitle("WhatsWhat - Message")
	ww.view.SetVisibleChild(messageBox)
	ww.back.SetVisible(true)
	ww.view.current = ErrorView
}

func (ww *whatsWhat) ViewQrCode(msg *ViewMessage) {
	fmt.Println("ViewQrCode: ", msg)

	ww.view.current = QrView
	if ww.view.qr.qrImage != nil {
		ww.view.qr.view.Remove(ww.view.qr.qrImage)
	}

	if ww.view.qr.qrChan == nil {
		var err error
		ww.view.qr.qrChan, err = ww.client.GetQRChannel(ww.view.qr.ctx)
		if err != nil {
			ww.queueViewMessage(ErrorView, err)
			return
		}

		if err = ww.client.Connect(); err != nil {
			ww.queueViewMessage(ErrorView, err)
			return
		}

		go ww.consumeQrMessages()
	}

	var code *string
	if msg.Error != nil {
		ww.view.qr.description.SetLabel(msg.Error.Error())
		ww.view.qr.retry.SetVisible(true)
		return
	} else if msg.Payload == nil {
		code = nil
	} else {
		codeStr := msg.Payload.(string)
		code = &codeStr
		ww.view.qr.description.SetLabel("Scan the WhatsApp QR code on your phone to login")
		ww.view.qr.retry.SetVisible(false)
	}

	ww.window.SetTitle("WhatsApp - Login")
	ww.back.SetVisible(true)

	var imageUi *gtk.Image
	if code == nil {
		fmt.Println("Empty code, load icon to show loading...")

		imageUi = gtk.NewImageFromIconName("image-missing-symbolic")
	} else {
		fmt.Println("Encoding qrcode bytes...")
		var qrCodeBytes []byte
		qrCodeBytes, err := qrcode.Encode(*code, qrcode.Medium, 512)
		if err != nil {
			ww.queueViewMessage(ErrorView, err.Error())
			return
		}

		fmt.Println("Making texture from QR code PNG bytes")
		qrCodeGlibBytes := glib.NewBytes(qrCodeBytes)
		texture, err := gdk.NewTextureFromBytes(qrCodeGlibBytes)
		if err != nil {
			ww.queueViewMessage(ErrorView, err.Error())
			return
		}
		fmt.Println("Making image from paintable texture")
		imageUi = gtk.NewImageFromPaintable(texture)
	}
	imageUi.SetVExpand(true)
	imageUi.SetHExpand(true)

	fmt.Println("Setting qrImage in UI")
	ww.view.qr.qrImage = imageUi
	ww.view.qr.view.Prepend(ww.view.qr.qrImage)
	ww.view.qr.view.SetVisible(true)
	ww.view.qr.SetVisible(true)
	ww.view.SetVisibleChild(ww.view.qr)
	ww.view.qr.qrImage.SetVisible(true)
}

func (ww *whatsWhat) Shutdown() {
	if ww.client != nil {
		ww.client.Disconnect()
	}
}

func (ww *whatsWhat) queueViewMessage(view View, payload interface{}) {
	var msgErr error
	switch payload.(type) {
	case error:
		msgErr = payload.(error)
		payload = nil
	default:
		msgErr = nil
	}
	ww.viewChan <- ViewMessage{
		View:    view,
		Payload: payload,
		Error:   msgErr,
	}
}

func (ww *whatsWhat) InitializeChat(ctx context.Context) {
	dbLog := wlog.Stdout("Database", "DEBUG", true)
	container, err := sqlstore.New("sqlite3", "file:whatswhat.db?_foreign_keys=on", dbLog)
	if err != nil {
		ww.queueViewMessage(ErrorView, err)
		return
	}

	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		ww.queueViewMessage(ErrorView, err)
		return
	}

	clientLog := wlog.Stdout("Client", "DEBUG", true)
	ww.client = whatsmeow.NewClient(deviceStore, clientLog)
	ww.client.AddEventHandler(eventHandler)

	// New login?
	if ww.client.Store.ID == nil {
		// initially set QR view without code
		ww.queueViewMessage(QrView, nil)
	} else {
		// Already logged in, connect
		if err = ww.client.Connect(); err != nil {
			ww.queueViewMessage(ErrorView, err.Error())
			return
		}

		ww.queueViewMessage(ChatView, nil)
	}
}

func main() {
	// Initialize UI
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	app := gtk.NewApplication("com.github.tiggilyboo.whatswhat", gio.ApplicationFlagsNone)
	app.ConnectActivate(func() {
		ww, err := newWhatsWhat(ctx, app)
		if err != nil {
			ww.queueViewMessage(ErrorView, err)
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
