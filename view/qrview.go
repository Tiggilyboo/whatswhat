package view

import (
	"context"
	"errors"
	"fmt"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
)

type QrUiView struct {
	*gtk.ScrolledWindow
	parent      UiParent
	view        *gtk.Box
	qrImage     *gtk.Image
	qrChan      <-chan whatsmeow.QRChannelItem
	description *gtk.Label
	refresh     *gtk.Button

	ctx    context.Context
	cancel context.CancelFunc
}

func NewQrUiView(parent UiParent) *QrUiView {
	ctx, cancel := context.WithCancel(context.Background())
	v := QrUiView{
		ctx:    ctx,
		cancel: cancel,
		parent: parent,
	}
	v.qrImage = gtk.NewImageFromIconName("image-missing-symbolic")
	v.qrImage.SetIconSize(gtk.IconSizeLarge)
	v.view = gtk.NewBox(gtk.OrientationVertical, 5)
	v.view.Append(v.qrImage)

	v.description = gtk.NewLabel("Loading...")
	v.description.SetVExpand(true)
	v.description.SetVExpand(true)
	v.view.Append(v.description)

	v.refresh = gtk.NewButtonFromIconName("update-symbolic")
	v.refresh.ConnectClicked(func() {
		v.RefreshQrCode()
	})
	v.view.Append(v.refresh)

	viewport := gtk.NewViewport(nil, nil)
	viewport.SetScrollToFocus(true)
	viewport.SetChild(v.view)

	v.ScrolledWindow = gtk.NewScrolledWindow()
	v.ScrolledWindow.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	v.ScrolledWindow.SetChild(viewport)
	v.ScrolledWindow.SetPropagateNaturalHeight(true)

	return &v
}

func (qr *QrUiView) Title() string {
	return "WhatsWhat - Login"
}

func (m *QrUiView) Done() <-chan struct{} {
	return m.ctx.Done()
}

func (m *QrUiView) Close() {
	if m.ctx.Done() != nil {
		m.cancel()
	}
}

func (qr *QrUiView) ShowError(err error) {
	qr.description.SetLabel(err.Error())
	qr.refresh.SetVisible(true)
}

func (qr *QrUiView) RefreshQrCode() {
	qr.parent.QueueMessage(QrView, nil)
}

func (qr *QrUiView) Update(msg *UiMessage) error {

	// Are we already authenticated?
	client := qr.parent.GetChatClient()
	if client.IsLoggedIn() {
		qr.description.SetLabel("Already logged in.")
		qr.refresh.SetVisible(false)
		qr.Close()
		return nil
	}

	var code *string
	if msg.Error != nil {
		return errors.New(fmt.Sprintf("QrUiView.Update passed error: %s", msg))
	} else if msg.Payload != nil {
		codeStr := msg.Payload.(string)
		code = &codeStr
	}

	if code != nil {

		// Update the label
		qr.description.SetLabel("Scan the WhatsApp QR code on your phone to login")
		qr.refresh.SetVisible(false)

	} else {

		// Set up QR view
		qr.description.SetLabel("Loading QR code...")
		qr.refresh.SetVisible(true)

		qr.Close()
		qr.ctx, qr.cancel = context.WithCancel(context.Background())

		var err error
		qr.qrChan, err = client.GetQRChannel(qr.ctx)
		if err != nil {
			return err
		}

		if err = client.Connect(); err != nil {
			return err
		}

		// Consume any new QR messages in another routine
		// This routine will automatically exit when the context has been cancelled
		go qr.consumeQrMessages()
	}

	var imageUi *gtk.Image
	if code == nil {
		fmt.Println("Empty code, load icon to show loading...")
		imageUi = gtk.NewImageFromIconName("image-missing-symbolic")
		imageUi.SetIconSize(gtk.IconSizeLarge)
	} else {
		fmt.Println("Encoding qrcode bytes...")
		var qrCodeBytes []byte
		qrCodeBytes, err := qrcode.Encode(*code, qrcode.Medium, 512)
		if err != nil {
			return err
		}

		fmt.Println("Making texture from QR code PNG bytes")
		qrCodeGlibBytes := glib.NewBytes(qrCodeBytes)
		texture, err := gdk.NewTextureFromBytes(qrCodeGlibBytes)
		if err != nil {
			return err
		}
		fmt.Println("Making image from paintable texture")
		imageUi = gtk.NewImageFromPaintable(texture)
	}
	imageUi.SetVExpand(true)
	imageUi.SetHExpand(true)

	// Remove existing qrImage
	qr.view.Remove(qr.qrImage)

	fmt.Println("Setting qrImage in UI")
	qr.qrImage = imageUi
	qr.view.Prepend(qr.qrImage)
	qr.view.SetVisible(true)
	qr.SetVisible(true)
	qr.qrImage.SetVisible(true)

	return nil
}

func (qr *QrUiView) success() {
	qr.refresh.SetVisible(false)
	qr.qrImage.SetVisible(false)
	qr.description.SetLabel("Success! Loading...")

	qr.parent.QueueMessage(ChatView, nil)
}

func (qr *QrUiView) consumeQrMessages() {
	for {
		select {
		case <-qr.ctx.Done():
			return

		case evt, ok := <-qr.qrChan:
			if !ok {
				// channel closed, exit
				fmt.Println("QR channel closed, exiting")
				return
			}
			fmt.Println("QR channel received: ", evt)

			if evt.Error != nil {
				qr.Update(&UiMessage{
					Identifier: QrView,
					Error:      evt.Error,
				})
				break
			}
			switch evt.Event {
			case "code":
				qr.Update(&UiMessage{
					Identifier: QrView,
					Payload:    evt.Code,
				})
			case "success":
				qr.success()
			default:
				fmt.Println("Login event: ", evt.Event)
			}
		}
	}
}
