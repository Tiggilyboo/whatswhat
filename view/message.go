package view

import (
	"context"
	"fmt"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
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
	v := MessageUiView{
		ctx:     ctx,
		cancel:  cancel,
		parent:  parent,
		Box:     gtk.NewBox(gtk.OrientationVertical, 0),
		message: gtk.NewLabel(""),
	}
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
	if m.ctx.Done() != nil {
		m.cancel()
	}
}

func (m *MessageUiView) Update(msg *UiMessage) (Response, error) {
	fmt.Println("MessageUiView.Update: ", msg)

	switch payload := msg.Payload.(type) {
	case error:
		m.message.SetLabel(payload.Error())
	default:
		m.message.SetLabel(fmt.Sprint(msg.Payload))
	}

	return msg.Intent, nil
}
