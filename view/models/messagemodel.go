package models

import (
	"fmt"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
)

type MessageModel struct {
	types.MessageInfo
	ChatJID   types.JID
	SenderJID types.JID
	Timestamp time.Time
	Message   string
}

func GetMessageModel(client *whatsmeow.Client, chatJID types.JID, msg *waWeb.WebMessageInfo) (*MessageModel, error) {
	evt, err := client.ParseWebMessage(chatJID, msg)
	if err != nil {
		return nil, err
	}
	if evt.Message == nil {
		return nil, fmt.Errorf("Message event has no message: %s", evt.Info.SourceString())
	}
	var msgText string
	switch {
	case evt.Message.Conversation != nil, msg.Message.ExtendedTextMessage != nil:
		msgText = evt.Message.GetConversation()
	case evt.Message.TemplateMessage != nil:
		tplMsg := evt.Message.GetTemplateMessage()
		tpl := tplMsg.GetHydratedTemplate()
		if tpl == nil {
			return nil, fmt.Errorf("Unable to read template message in %s", chatJID)
		}
		// TODO
		msgText = tpl.GetHydratedContentText()
	case evt.Message.HighlyStructuredMessage != nil:
		tplMsg := evt.Message.GetHighlyStructuredMessage()
		tpl := tplMsg.GetHydratedHsm()
		if tpl == nil {
			return nil, fmt.Errorf("Unable to read structured template Hsm message in %s", chatJID)
		}
		// TODO
		tpl4r := tpl.GetHydratedTemplate()
		if tpl4r == nil {
			return nil, fmt.Errorf("Unable to read structure template message")
		}
		msgText = tpl4r.GetHydratedContentText()
	case evt.Message.TemplateButtonReplyMessage != nil:
		// TODO
		msgText = evt.Message.TemplateButtonReplyMessage.GetSelectedDisplayText()
	case evt.Message.ListMessage != nil:
		lstMsg := evt.Message.ListMessage
		// TODO
		msgText = lstMsg.GetDescription()
	case evt.Message.ListResponseMessage != nil:
		lstMsg := evt.Message.ListResponseMessage
		// TODO
		msgText = lstMsg.GetDescription()
	default:
		return nil, fmt.Errorf("Currently unsupported message type")
	}

	model := MessageModel{
		MessageInfo: evt.Info,
		Message:     msgText,
	}

	return &model, nil
}
