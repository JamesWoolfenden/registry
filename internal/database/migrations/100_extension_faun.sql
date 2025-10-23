-- adding in support for faun meta data
-- and JSONB columns for complex nested data structures
alter table servers
    add faun jsonb;

-- we dont need this for now as it just causes problems - todo
drop index idx_unique_latest_per_server;

create index idx_unique_latest_per_server
    on servers (server_name)
    where (is_latest = true);
