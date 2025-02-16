package view

import (
	"context"
	"fmt"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

type MessageUiView struct {
	*gtk.Box
	parent  UiParent
	message *gtk.Label
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewMessageView(parent UiParent) *MessageUiView {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	v := MessageUiView{
		ctx:     ctx,
		cancel:  cancel,
		parent:  parent,
		Box:     gtk.NewBox(gtk.OrientationVertical, 0),
		message: gtk.NewLabel(""),
	}
	v.message.SetSingleLineMode(false)
	v.message.SetWrapMode(pango.WrapWordChar)
	v.message.SetWrap(true)

	v.Append(v.message)

	return &v
}

func (m *MessageUiView) Title() string {
	return "WhatsWhat - Message"
}

func (m *MessageUiView) Done() <-chan struct{} {
	return m.ctx.Done()
}

func (m *MessageUiView) Close() {
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *MessageUiView) WaitDoneThenClose(returnTo Message, intent Response) {
	<-m.Done()
	m.parent.QueueMessageWithIntent(returnTo, nil, ResponseBackView)
}

func (m *MessageUiView) Update(msg *UiMessage) (Response, error) {
	fmt.Println("MessageUiView.Update: ", msg)

	switch payload := msg.Payload.(type) {
	case error:
		m.message.SetLabel(payload.Error())
	case *UiMessage:
		m.message.SetLabel("Loading...")

		// Wait for context to complete then go to passed view
		payloadContext := payload.Payload.(context.Context)
		m.ctx, m.cancel = context.WithCancel(payloadContext)
		go m.WaitDoneThenClose(payload.Identifier, payload.Intent)

	case nil:
		m.message.SetLabel("Loading...")

	default:
		fmt.Printf("Assuming message payload is string: %v\n", payload)
		m.message.SetLabel(fmt.Sprint(msg.Payload))
	}

	return msg.Intent, nil
}
