package models

import (
	"go.mau.fi/whatsmeow/types"
	"time"
)

type OnDemandRequestInfo struct {
	ChatJID types.JID
	Error   error
	MinTime time.Time
}

func (o OnDemandRequestInfo) JID() *types.JID {
	return &o.ChatJID
}
func (o OnDemandRequestInfo) Err() error {
	return o.Error
}
