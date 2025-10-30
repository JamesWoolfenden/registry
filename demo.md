# MCP Registry

## Start application

(Starts 3 containers appserver, postgreSQL and pgweb )
 
```shell
 cd ~/code/registry
 make dev-compose
 ```

## Upstream

<https://registry.modelcontextprotocol.io/v0/servers?search=ai.aliengiraffe/spotdb>

## Local

Nothing up our sleeves: (Show empty API and database)

<http://localhost:8080/v0/servers>

<http://0.0.0.0:8081/>

## Analysis

By intercepting the import process.

But first the export from the Upstream...

```shell
cd ~/code/registry
fetch --export-data production_servers.json
```

Finally the import (cut-down export for the demo)

```shell
load -e production_dodgy.json
```

<http://0.0.0.0:8081/>

Show data in DB and display the view

Show data in API
<http://localhost:8080/v0/servers>

(note the extra data)