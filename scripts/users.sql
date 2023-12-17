update
    users
set audio_max_size    = 20971520,
    video_max_size    = 52428800,
    playlist_max_size = 3
where tg_user_id = 0;
