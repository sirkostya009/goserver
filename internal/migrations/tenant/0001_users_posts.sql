create table users (
    id         uuid        primary key default uuidv7(),
    name       text        not null,
    email      text        not null unique,
    created_at timestamptz not null default now()
);

create table posts (
    id         uuid        primary key default uuidv7(),
    author_id  uuid        not null references users (id) on delete cascade,
    title      text        not null,
    body       text        not null default '',
    created_at timestamptz not null default now()
);

create index posts_author_idx on posts (author_id);
