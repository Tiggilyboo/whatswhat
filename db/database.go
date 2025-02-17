package db

import (
	"github.com/rs/zerolog"
	"github.com/tiggilyboo/whatswhat/db/migrations"
	"go.mau.fi/util/dbutil"
)

type Database struct {
	*dbutil.Database
	Conversation *ConversationQuery
	Message      *MessageQuery
	PushName     *PushNameQuery
}

func New(db *dbutil.Database, log zerolog.Logger) *Database {
	db = db.Child("whatswhat_version", migrations.Table, dbutil.ZeroLogger(log))

	return &Database{
		Database: db,
		Conversation: &ConversationQuery{
			QueryHelper: dbutil.MakeQueryHelper(db, func(_ *dbutil.QueryHelper[*Conversation]) *Conversation {
				return &Conversation{}
			}),
		},
		Message: &MessageQuery{
			Database: db,
		},
		PushName: &PushNameQuery{
			Database: db,
		},
	}
}
