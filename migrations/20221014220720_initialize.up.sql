create table users
(
    id                integer primary key autoincrement not null,
    tg_user_id        integer unique                    not null,
    -- Maximum allowable size for downloading audio for a user.
    audio_max_size    integer                           not null default 0,
    -- Maximum allowable size for downloading video for a user.
    video_max_size    integer                           not null default 0,
    -- Maximum allowable videos length to download from playlist.
    playlist_max_size integer                           not null default 0,
    -- Last message from the user in the bot.
    last_event_at     datetime                          not null default current_timestamp,
    created_at        datetime                          not null default current_timestamp
);

create table media
(
    id            integer primary key autoincrement                    not null,
    user_id       integer references users (id) on delete cascade      not null,
    -- Telegram message id in telegram to understand to what message we need to reply.
    -- We can't make it unique because if we download media for playlist all medias will have same tg message id.
    tg_message_id integer                                              not null,
    -- Uri to download video.
    uri           text                                                 not null,
    title         text                                                 not null default '',
    state         text check ( state in ('pending', 'error', 'done') ) not null default 'pending',
    type          text check ( type in ('audio', 'video') )            not null,
    -- When media was successfully downloaded.
    done_at       datetime                                                      default null,
    updated_at    datetime                                             not null default current_timestamp,
    created_at    datetime                                             not null default current_timestamp
);
