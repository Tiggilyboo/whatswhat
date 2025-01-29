package db

import (
	"fmt"

	"github.com/rs/zerolog"
	"github.com/tiggilyboo/whatswhat/db/migrations"
	"go.mau.fi/util/dbutil"
	"go.mau.fi/whatsmeow/types"
)

type Database struct {
	*dbutil.Database
	Conversation *ConversationQuery
	Message      *MessageQuery
}

func New(deviceJID types.JID, db *dbutil.Database, log zerolog.Logger) *Database {
	fmt.Println("db.New: ", deviceJID)

	db = db.Child("whatsmeow_version", migrations.Table, dbutil.ZeroLogger(log))

	return &Database{
		Database: db,
		Conversation: &ConversationQuery{
			DeviceJID: deviceJID,
			QueryHelper: dbutil.MakeQueryHelper(db, func(_ *dbutil.QueryHelper[*Conversation]) *Conversation {
				return &Conversation{}
			}),
		},
		Message: &MessageQuery{
			DeviceJID: deviceJID,
			Database:  db,
		},
	}
}
