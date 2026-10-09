-- null for users made before passwords existed: they cannot log in until given one
alter table users add column password_hash text;

-- tokens are kept as the sha-256 of what the client holds, which is enough to
-- find one and useless to present
create table sessions (
    id         uuid        primary key default uuidv7(),
    user_id    uuid        not null references users (id) on delete cascade,
    token_hash bytea       not null unique,
    created_at timestamptz not null default now(),
    expires_at timestamptz not null
);

create index sessions_user_idx on sessions (user_id);

create table api_keys (
    id         uuid        primary key default uuidv7(),
    user_id    uuid        not null references users (id) on delete cascade,
    name       text        not null,
    token_hash bytea       not null unique,
    created_at timestamptz not null default now(),
    expires_at timestamptz
);

create index api_keys_user_idx on api_keys (user_id);
