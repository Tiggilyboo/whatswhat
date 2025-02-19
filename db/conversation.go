package db

import (
	"context"
	"database/sql"
	"time"

	"go.mau.fi/util/dbutil"
	"go.mau.fi/util/ptr"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
)

type ConversationQuery struct {
	*dbutil.QueryHelper[*Conversation]
}

type Conversation struct {
	DeviceJID                 types.JID
	ChatJID                   types.JID
	LastMessageTimestamp      time.Time
	Archived                  *bool
	Pinned                    *bool
	MuteEndTime               time.Time
	EndOfHistoryTransferType  *waHistorySync.Conversation_EndOfHistoryTransferType
	EphemeralExpiration       *uint32
	EphemeralSettingTimestamp *int64
	MarkedAsUnread            *bool
	UnreadCount               *uint32
	Name                      string
}

func parseHistoryTime(ts *uint64) time.Time {
	if ts == nil || *ts == 0 {
		return time.Time{}
	}
	return time.Unix(int64(*ts), 0)
}

func NewConversation(deviceJID types.JID, chatJID types.JID, fallbackChatName string, conv *waHistorySync.Conversation) *Conversation {
	var pinned *bool
	if conv.Pinned != nil {
		pinned = ptr.Ptr(*conv.Pinned > 0)
	}
	var convoName string
	if conv.Name != nil {
		convoName = conv.GetName()
	} else if conv.DisplayName != nil {
		convoName = conv.GetDisplayName()
	} else if conv.Username != nil {
		convoName = conv.GetUsername()
	} else {
		convoName = fallbackChatName
	}
	return &Conversation{
		DeviceJID:                 deviceJID,
		ChatJID:                   chatJID,
		LastMessageTimestamp:      parseHistoryTime(conv.LastMsgTimestamp),
		Archived:                  conv.Archived,
		Pinned:                    pinned,
		MuteEndTime:               parseHistoryTime(conv.MuteEndTime),
		EndOfHistoryTransferType:  conv.EndOfHistoryTransferType,
		EphemeralExpiration:       conv.EphemeralExpiration,
		EphemeralSettingTimestamp: conv.EphemeralSettingTimestamp,
		MarkedAsUnread:            conv.MarkedAsUnread,
		UnreadCount:               conv.UnreadCount,
		Name:                      convoName,
	}
}

const (
	upsertHistorySyncConversationQuery = `INSERT INTO whatsapp_history_sync_conversation (
			device_jid, chat_jid, last_message_timestamp, archived, pinned, mute_end_time,
			end_of_history_transfer_type, ephemeral_expiration, ephemeral_setting_timestamp, marked_as_unread,
			unread_count, name
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (device_jid, chat_jid)
		DO UPDATE SET
			last_message_timestamp=CASE
				WHEN whatsapp_history_sync_conversation.last_message_timestamp IS NULL
				         OR (excluded.last_message_timestamp IS NOT NULL AND excluded.last_message_timestamp > whatsapp_history_sync_conversation.last_message_timestamp)
					THEN excluded.last_message_timestamp
				ELSE whatsapp_history_sync_conversation.last_message_timestamp
			END,
			archived=COALESCE(excluded.archived, whatsapp_history_sync_conversation.archived),
			pinned=COALESCE(excluded.pinned, whatsapp_history_sync_conversation.pinned),
			mute_end_time=COALESCE(excluded.mute_end_time, whatsapp_history_sync_conversation.mute_end_time),
			end_of_history_transfer_type=COALESCE(excluded.end_of_history_transfer_type, whatsapp_history_sync_conversation.end_of_history_transfer_type),
			ephemeral_expiration=COALESCE(excluded.ephemeral_expiration, whatsapp_history_sync_conversation.ephemeral_expiration),
			ephemeral_setting_timestamp=COALESCE(excluded.ephemeral_setting_timestamp, whatsapp_history_sync_conversation.ephemeral_setting_timestamp),
			marked_as_unread=COALESCE(excluded.marked_as_unread, whatsapp_history_sync_conversation.marked_as_unread),
			unread_count=COALESCE(excluded.unread_count, whatsapp_history_sync_conversation.unread_count),
			name=COALESCE(excluded.name, whatsapp_history_sync_conversation.name)
	`
	getRecentConversations = `
		SELECT
			device_jid, chat_jid, last_message_timestamp, archived, pinned, mute_end_time,
			end_of_history_transfer_type, ephemeral_expiration, ephemeral_setting_timestamp, marked_as_unread,
			unread_count, name
		FROM whatsapp_history_sync_conversation c
		WHERE device_jid=$1 AND archived=$2
		ORDER BY last_message_timestamp DESC
		LIMIT $3
	`
	getConversationByJID = `
		SELECT
			device_jid, chat_jid, last_message_timestamp, archived, pinned, mute_end_time,
			end_of_history_transfer_type, ephemeral_expiration, ephemeral_setting_timestamp, marked_as_unread,
			unread_count, name
		FROM whatsapp_history_sync_conversation
		WHERE device_jid=$1 AND chat_jid=$2
	`
	deleteAllConversationsQuery = "DELETE FROM whatsapp_history_sync_conversation WHERE device_jid=$1"
	deleteConversationQuery     = `
		DELETE FROM whatsapp_history_sync_conversation
		WHERE device_jid=$1 AND chat_jid=$2
	`

	updateConversationLastMessage = `
		UPDATE whatsapp_history_sync_conversation
		SET last_message_timestamp = (
			SELECT MAX(timestamp)
			FROM whatsapp_history_sync_message
			WHERE device_jid=$1 AND chat_jid=$2
		)
		WHERE device_jid=$1 AND chat_jid=$2
	`
)

func (c *Conversation) sqlVariables() []any {
	var lastMessageTS, muteEndTime *int64
	if !c.LastMessageTimestamp.IsZero() {
		lastMessageTS = ptr.Ptr(c.LastMessageTimestamp.Unix())
	}
	if !c.MuteEndTime.IsZero() {
		muteEndTime = ptr.Ptr(c.MuteEndTime.Unix())
	}
	return []any{
		c.DeviceJID,
		c.ChatJID,
		lastMessageTS,
		c.Archived,
		c.Pinned,
		muteEndTime,
		c.EndOfHistoryTransferType,
		c.EphemeralExpiration,
		c.EphemeralSettingTimestamp,
		c.MarkedAsUnread,
		c.UnreadCount,
		c.Name,
	}
}

func (cq *ConversationQuery) Put(ctx context.Context, deviceJID types.JID, conv *Conversation) error {
	conv.DeviceJID = deviceJID
	return cq.Exec(ctx, upsertHistorySyncConversationQuery, conv.sqlVariables()...)
}

func (cq *ConversationQuery) GetRecent(ctx context.Context, deviceJID types.JID, limit int, archived bool) ([]*Conversation, error) {
	limitPtr := &limit
	// Negative limit on SQLite means unlimited, but Postgres prefers a NULL limit.
	if limit < 0 && cq.GetDB().Dialect == dbutil.Postgres {
		limitPtr = nil
	}
	return cq.QueryMany(ctx, getRecentConversations, deviceJID, archived, limitPtr)
}

func (cq *ConversationQuery) Get(ctx context.Context, deviceJID types.JID, chatJID types.JID) (*Conversation, error) {
	return cq.QueryOne(ctx, getConversationByJID, deviceJID, chatJID)
}

func (cq *ConversationQuery) DeleteAll(ctx context.Context, deviceJID types.JID) error {
	return cq.Exec(ctx, deleteAllConversationsQuery, deviceJID)
}

func (cq *ConversationQuery) Delete(ctx context.Context, deviceJID types.JID, chatJID types.JID) error {
	return cq.Exec(ctx, deleteConversationQuery, deviceJID, chatJID)
}

func (cq *ConversationQuery) UpdateLastMessageTimestamp(ctx context.Context, deviceJID types.JID, chatJID types.JID) error {
	return cq.Exec(ctx, updateConversationLastMessage, deviceJID, chatJID)
}

func (c *Conversation) Scan(row dbutil.Scannable) (*Conversation, error) {
	var lastMessageTS, muteEndTime sql.NullInt64
	err := row.Scan(
		&c.DeviceJID,
		&c.ChatJID,
		&lastMessageTS,
		&c.Archived,
		&c.Pinned,
		&muteEndTime,
		&c.EndOfHistoryTransferType,
		&c.EphemeralExpiration,
		&c.EphemeralSettingTimestamp,
		&c.MarkedAsUnread,
		&c.UnreadCount,
		&c.Name,
	)
	if err != nil {
		return nil, err
	}
	if lastMessageTS.Int64 != 0 {
		c.LastMessageTimestamp = time.Unix(lastMessageTS.Int64, 0)
	}
	if muteEndTime.Int64 != 0 {
		c.MuteEndTime = time.Unix(muteEndTime.Int64, 0)
	}
	return c, nil
}
