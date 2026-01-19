-- +goose Up
-- +goose StatementBegin
create extension if not exists "uuid-ossp";
create table numbers (
    id uuid primary key default uuid_generate_v4(),
    number integer not null
);
create index idx_numbers_number on numbers (number);
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
drop index idx_numbers_number;
drop table numbers;
drop extension if exists "uuid-ossp";
-- +goose StatementEnd