-- v0 -> v1: Initial schema

CREATE TABLE whatsapp_history_sync_conversation (
    device_jid                   TEXT    NOT NULL,
    chat_jid                     TEXT    NOT NULL,

    last_message_timestamp       BIGINT,
    archived                     BOOLEAN,
    pinned                       BOOLEAN,
    mute_end_time                BIGINT,
    end_of_history_transfer_type INTEGER,
    ephemeral_expiration         INTEGER,
    ephemeral_setting_timestamp  BIGINT,
    marked_as_unread             BOOLEAN,
    unread_count                 INTEGER,
    name                         TEXT    NOT NULL,

    PRIMARY KEY (device_jid, chat_jid)
);

CREATE TABLE whatsapp_history_sync_message (
    device_jid    TEXT   NOT NULL,
    chat_jid      TEXT   NOT NULL,
    sender_jid    TEXT   NOT NULL,
    message_id    TEXT   NOT NULL,
    timestamp     BIGINT NOT NULL,
    data          bytea  NOT NULL,

    PRIMARY KEY (device_jid, chat_jid, sender_jid, message_id),
    CONSTRAINT whatsapp_history_sync_message_conversation_fkey FOREIGN KEY (device_jid, chat_jid)
    REFERENCES whatsapp_history_sync_conversation (device_jid, chat_jid) ON UPDATE CASCADE ON DELETE CASCADE
);

CREATE TABLE whatsapp_history_sync_pushname (
    device_jid   TEXT NOT NULL,
    jid          TEXT NOT NULL,
    name         TEXT NOT NULL,

    PRIMARY KEY (device_jid, jid)
);
