package models

import (
	"encoding/base64"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type MessageType uint8

const (
	MessageTypeUnknown MessageType = iota
	MessageTypeText
	MessageTypeImage
	MessageTypeAudio
	MessageTypeVideo
	MessageTypeDocument
)

type MessageModel struct {
	types.MessageInfo
	Type       MessageType
	Message    string
	URL        *string
	MediaBytes *[]byte
}

func GetMessageModel(client *whatsmeow.Client, chatJID types.JID, msg *waWeb.WebMessageInfo) (*MessageModel, error) {
	evt, err := client.ParseWebMessage(chatJID, msg)
	if err != nil {
		return nil, err
	}
	emsg := evt.Message

	model := MessageModel{
		MessageInfo: evt.Info,
		Type:        MessageTypeUnknown,
	}

	switch {
	case emsg == nil:
		model.Type = MessageTypeText
		model.Message = "Unable to parse message"
	case emsg.Conversation != nil, msg.Message.ExtendedTextMessage != nil:
		model.Type = MessageTypeText
		model.Message = emsg.GetConversation()
	case emsg.TemplateMessage != nil:
		tplMsg := emsg.GetTemplateMessage()
		tpl := tplMsg.GetHydratedTemplate()
		if tpl == nil {
			return nil, fmt.Errorf("Unable to read template message in %s", chatJID)
		}
		model.Type = MessageTypeText
		model.Message = tpl.GetHydratedContentText()
	case emsg.HighlyStructuredMessage != nil:
		tplMsg := emsg.GetHighlyStructuredMessage()
		tpl := tplMsg.GetHydratedHsm()
		if tpl == nil {
			return nil, fmt.Errorf("Unable to read structured template Hsm message in %s", chatJID)
		}
		tpl4r := tpl.GetHydratedTemplate()
		if tpl4r == nil {
			return nil, fmt.Errorf("Unable to read structure template message")
		}
		model.Type = MessageTypeText
		model.Message = tpl4r.GetHydratedContentText()
	case emsg.TemplateButtonReplyMessage != nil:
		model.Type = MessageTypeText
		model.Message = emsg.TemplateButtonReplyMessage.GetSelectedDisplayText()
	case emsg.ListMessage != nil:
		lstMsg := emsg.ListMessage
		model.Type = MessageTypeText
		model.Message = lstMsg.GetDescription()
	case emsg.ListResponseMessage != nil:
		lstMsg := emsg.ListResponseMessage
		model.Type = MessageTypeText
		model.Message = lstMsg.GetDescription()
	case emsg.ImageMessage != nil:
		imgMsg := emsg.ImageMessage
		model.Type = MessageTypeImage
		model.URL = imgMsg.URL
		model.MediaBytes = &imgMsg.JPEGThumbnail
		if imgMsg.Caption != nil {
			model.Message = *imgMsg.Caption
		} else {
			model.Message = ""
		}
	case emsg.AudioMessage != nil:
		audmsg := emsg.AudioMessage
		model.Type = MessageTypeAudio
		model.URL = audmsg.URL
		model.MediaBytes = &audmsg.Waveform
		if audmsg.URL != nil {
			model.Message = *audmsg.URL
		} else {
			model.Message = "Audio"
		}
	case emsg.StickerMessage != nil:
		stkmsg := emsg.StickerMessage
		model.Type = MessageTypeImage
		model.URL = stkmsg.URL
		model.MediaBytes = &stkmsg.PngThumbnail
		model.Message = "Sticker"
	case emsg.VideoMessage != nil:
		vidmsg := emsg.VideoMessage
		model.Type = MessageTypeVideo
		model.URL = vidmsg.URL
		if vidmsg.Caption != nil {
			model.Message = *vidmsg.Caption
		} else {
			model.Message = "Video Attachment"
		}
	case emsg.PtvMessage != nil:
		ptvmsg := emsg.PtvMessage
		model.Type = MessageTypeVideo
		model.URL = ptvmsg.URL
		model.MediaBytes = &ptvmsg.JPEGThumbnail
		if ptvmsg.Caption != nil {
			model.Message = *ptvmsg.Caption
		} else {
			model.Message = "Video Message"
		}
	case emsg.DocumentMessage != nil:
		docmsg := emsg.DocumentMessage
		model.Type = MessageTypeDocument
		model.URL = docmsg.URL
		if docmsg.Caption != nil {
			model.Message = *docmsg.Caption
		} else if docmsg.FileName != nil {
			model.Message = *docmsg.FileName
		} else if docmsg.Title == nil {
			model.Message = *docmsg.Title
		}
	default:
		data, _ := proto.Marshal(emsg)
		encodedMsg := base64.StdEncoding.EncodeToString(data)
		model.Message = fmt.Sprintf("Error parsing: %s", encodedMsg)
	}

	return &model, nil
}
