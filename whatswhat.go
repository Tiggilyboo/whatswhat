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
	ctx    context.Context
	parent *whatsWhat
	view   *gtk.Box

	cancel context.CancelFunc
}

type qrView struct {
	*gtk.ScrolledWindow
	ctx     context.Context
	parent  *whatsWhat
	view    *gtk.Box
	qrImage *gtk.Image

	cancel context.CancelFunc
}

type ViewMessage struct {
	view    View
	payload interface{}
}

type whatsWhat struct {
	*gtk.Application
	window   *gtk.ApplicationWindow
	header   *gtk.HeaderBar
	back     *gtk.Button
	client   *whatsmeow.Client
	viewChan chan ViewMessage

	view struct {
		*gtk.Stack
		current View
		chats   *chatView
		qr      *qrView
	}
}

func newChatView(ctx context.Context, ww *whatsWhat) *chatView {
	v := chatView{
		ctx:    ctx,
		parent: ww,
	}
	v.view = gtk.NewBox(gtk.OrientationVertical, 0)

	viewport := gtk.NewViewport(nil, nil)
	viewport.SetScrollToFocus(true)
	viewport.SetChild(v.view)

	v.ScrolledWindow = gtk.NewScrolledWindow()
	v.ScrolledWindow.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	v.ScrolledWindow.SetChild(viewport)
	v.ScrolledWindow.SetPropagateNaturalHeight(true)

	return &v
}

func newQrView(ctx context.Context, ww *whatsWhat) *qrView {
	v := qrView{
		ctx:    ctx,
		parent: ww,
	}
	v.qrImage = gtk.NewImageFromIconName("image-missing-symbolic")
	v.qrImage.SetIconSize(gtk.IconSizeLarge)
	v.view = gtk.NewBox(gtk.OrientationVertical, 5)
	v.view.Append(v.qrImage)

	description := gtk.NewLabel("Scan the WhatsApp QR code on your phone to login")
	description.SetVExpand(true)
	description.SetVExpand(true)
	v.view.Append(description)

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
	ww := whatsWhat{Application: app}

	ww.viewChan = make(chan ViewMessage, 10)
	ww.view.Stack = gtk.NewStack()
	ww.view.SetTransitionType(gtk.StackTransitionTypeSlideLeftRight)

	ww.view.qr = newQrView(ctx, &ww)
	ww.view.chats = newChatView(ctx, &ww)

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
		fmt.Println("consumeMessages: ", msg)

		switch msg.view {
		case ErrorView:
			text := msg.payload.(string)
			glib.IdleAdd(func() {
				ww.ViewError("Error", text)
			})
		case ChatView:
			glib.IdleAdd(func() {
				ww.ViewChats()
			})
		case QrView:
			code := msg.payload.(string)
			glib.IdleAdd(func() {
				ww.ViewQrCode(code)
			})
		}
	}
}

func (ww *whatsWhat) ViewChats() {
	fmt.Println("ViewChats: Invoked")

	ww.window.SetTitle("WhatsWhat - Chats")
	ww.view.SetVisibleChild(ww.view.chats)
	ww.back.SetVisible(false)
	ww.view.current = ChatView

	if ww.client == nil {
		return
	}
}

func (ww *whatsWhat) ViewError(title string, message string) {
	fmt.Println("ViewError: ", message)

	messageBox := gtk.NewBox(gtk.OrientationVertical, 0)
	text := gtk.NewLabel(message)
	messageBox.Append(text)

	ww.window.SetTitle(title)
	ww.view.SetVisibleChild(messageBox)
	ww.back.SetVisible(false)
	ww.view.current = ErrorView
}

func (ww *whatsWhat) ViewQrCode(code string) {
	fmt.Println("ViewQrCode: ", code)

	ww.view.current = QrView
	if ww.view.qr.qrImage != nil {
		ww.view.qr.view.Remove(ww.view.qr.qrImage)
	}

	ww.window.SetTitle("WhatsApp - Login")
	ww.back.SetVisible(true)

	fmt.Println("Encoding qrcode bytes...")
	var qrCodeBytes []byte
	qrCodeBytes, err := qrcode.Encode(code, qrcode.Medium, 512)
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
	imageUi := gtk.NewImageFromPaintable(texture)
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
	ww.viewChan <- ViewMessage{
		view:    view,
		payload: payload,
	}
}

func (ww *whatsWhat) InitializeChat(ctx context.Context) {
	dbLog := wlog.Stdout("Database", "DEBUG", true)
	container, err := sqlstore.New("sqlite3", "file:whatswhat.db?_foreign_keys=on", dbLog)
	if err != nil {
		ww.queueViewMessage(ErrorView, err.Error())
		return
	}

	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		ww.queueViewMessage(ErrorView, err.Error())
		return
	}

	clientLog := wlog.Stdout("Client", "DEBUG", true)
	ww.client = whatsmeow.NewClient(deviceStore, clientLog)
	ww.client.AddEventHandler(eventHandler)

	// New login?
	if ww.client.Store.ID == nil {
		qrChan, err := ww.client.GetQRChannel(ctx)
		if err != nil {
			ww.queueViewMessage(ErrorView, err.Error())
			return
		}
		if err = ww.client.Connect(); err != nil {
			ww.queueViewMessage(ErrorView, err.Error())
			return
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				ww.queueViewMessage(QrView, evt.Code)
			} else {
				fmt.Println("Login event: ", evt.Event)
			}
		}
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
			glib.IdleAdd(func() {
				ww.ViewError("Error", err.Error())
			})
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
