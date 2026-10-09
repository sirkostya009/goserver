create table tenants (
    id         uuid        primary key default uuidv7(),
    slug       text        not null unique,
    name       text        not null,
    created_at timestamptz not null default now()
);
