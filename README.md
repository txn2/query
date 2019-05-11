![query](https://raw.githubusercontent.com/txn2/query/master/mast.jpg)
[![query Release](https://img.shields.io/github/release/txn2/query.svg)](https://github.com/txn2/query/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/txn2/query)](https://goreportcard.com/report/github.com/txn2/query)
[![GoDoc](https://godoc.org/github.com/txn2/query?status.svg)](https://godoc.org/github.com/txn2/query)
[![Docker Container Image Size](https://shields.beevelop.com/docker/image/image-size/txn2/query/latest.svg)](https://hub.docker.com/r/txn2/query/)
[![Docker Container Layers](https://shields.beevelop.com/docker/image/layers/txn2/rxtx/latest.svg)](https://hub.docker.com/r/txn2/query/)

WIP: query TXN2 data by account, model and index pattern. Save queries and execute saved queries.

## Local Development

The project includes a Docker Compose file with Elasticsearch, Kibana and Cerebro:
```bash
docker-compose up
```

Add test account with [txn2/provision]:
```bash
curl -X POST \
  http://localhost:8070/account \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "test",
    "description": "This is a test account",
    "display_name": "Test Organization",
    "active": true,
    "access_keys": [
        {
            "name": "test",
            "key": "PDWgYr3bQGNoLptBRDkLTGQcRmCMqLGRFpXoXJ8xMPsMLMg3LHvWpJgDu2v3LYBA",
            "description": "Generic access key 2",
            "active": true
        }
    ],
    "modules": [
        "wx",
        "data_science"
    ]
}'
```

Add an admin [User] with with [txn2/provision] given access to the test account:

```bash
curl -X POST \
  http://localhost:8070/user \
  -H 'Content-Type: application/json' \
  -d '{
	"id": "test",
	"description": "Test User admin",
	"display_name": "Test User",
	"active": true,
	"sysop": false,
	"password": "eWidL7UtiWJABHgn8WA",
	"sections_all": false,
	"sections": ["api", "config", "data"],
	"accounts": ["test"],
	"admin_accounts": ["test"]
}'
```

Get a user [Token] from [txn2/provision] with the [User]'s id and password:

```bash
TOKEN=$(curl -s -X POST \
          http://localhost:8070/authUser?raw=true \
          -d '{
        	"id": "test_user",
        	"password": "eWidL7UtiWJABHgn8WA"
        }') && echo $TOKEN
```

Insert a sample [Model] into the **test** account using [txn2/tm] running on port **8085**:

```bash
curl -X POST http://localhost:8085/model/test \
 -H "Authorization: Bearer $TOKEN" \
 -H 'Content-Type: application/json' \
 -d '{
  "machine_name": "some_metrics",
  "display_name": "Some Metrics",
  "description": "A sample model describing some metrics sent through rxtx",
  "fields": [
    {
      "machine_name": "device_id",
      "display_name": "Device ID",
      "data_type": "keyword"
    },
    {
      "machine_name": "random_number",
      "display_name": "Random Number",
      "data_type": "integer"
    },
    {
      "machine_name": "another_number",
      "display_name": "Another Number",
      "data_type": "integer"
    }
  ]
}'
```

Within Elasticsearch there is now a template `_template/test-data-some_metrics` for the account **test** describing [txn2/rxtx]/[txn2/rtbeat] inbound data matching index pattern `test-data-some_metrics-*`. Send some sample data to Elasticsearch through [txn2/rxtx] and wait for the batch interval (specified in the docker-compose) to complete:

```bash
curl -X POST \
  http://localhost:8090/rx/test/some_metrics/sample-data \
  -H 'Content-Type: application/json' \
  -d "{
  \"device_id\": \"12345\",
  \"random_number\": \"${RANDOM}\",
  \"another_number\": 12345
}"
```

The fields in the data sent to [txn2/rxtx] should match the fields described in the [txn2/tm] [Model]. Although the value for "random_number" is represented here as a string, the template mapping added with the [Model] instructs Elasticsearch to index it as an integer.


## Example Queries

Run **query** from source. Configure it to use the services running from docker-compose above.

```bash
go run ./cmd/query.go --esServer=http://localhost:9200 --tokenKey="somegoodkey"
```

Run / Test a [Query]:
```bash
curl -X POST \
  http://localhost:8080/run/test \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "machine_name": "count_some_metrics",
    "display_name": "Get all records from the some_metrics index.",
    "description": "Return all matches",
    "model": "some_metrics",
    "idx_pattern": "-ts-*",
    "query": {
      "size": 0,
	  "query": {
	    "match_all": {}
	  }
	}
}'
```

Upsert a [Query] (must have admin access to account):
```bash
curl -X POST \
  http://localhost:8080/upsert/test \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "machine_name": "count_some_metrics",
    "display_name": "Get all records from the some_metrics index.",
    "description": "Return all matches",
    "model": "some_metrics",
    "idx_pattern": "-ts-*",
    "query": {
      "size": 0,
	  "query": {
	    "match_all": {}
	  }
	}
}'
```

Search for queries:
```bash
curl -X POST \
  http://localhost:8080/search/test \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
  "size": 10,
  "query": {
    "match_all": {}
  }
}'
```

Get a [Query]:
```bash
curl -X GET \
  http://localhost:8080/get/test/count_some_metrics \
  -H "Authorization: Bearer $TOKEN"
```

Execute a [Query]:
```bash
curl -X GET \
  http://localhost:8080/exec/test/count_some_metrics \
  -H "Authorization: Bearer $TOKEN"
```

[Token]: https://github.com/txn2/token
[txn2/provision]: https://github.com/txn2/provision
[txn2/tm]: https://github.com/txn2/tm
[txn2/rtbeat]: https://github.com/txn2/tm
[txn2/rxtx]: https://github.com/txn2/rxtx
[User]: https://godoc.org/github.com/txn2/provision#User
[Query]: https://godoc.org/github.com/txn2/query#Query
[Model]: https://godoc.org/github.com/txn2/tm#Model

## Release Packaging

Build test release:
```bash
goreleaser --skip-publish --rm-dist --skip-validate
```

Build and release:
```bash
GITHUB_TOKEN=$GITHUB_TOKEN goreleaser --rm-dist
```