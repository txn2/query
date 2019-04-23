![query](https://raw.githubusercontent.com/txn2/query/master/mast.jpg)
[![query Release](https://img.shields.io/github/release/txn2/query.svg)](https://github.com/txn2/query/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/txn2/query)](https://goreportcard.com/report/github.com/txn2/query)
[![GoDoc](https://godoc.org/github.com/txn2/query?status.svg)](https://godoc.org/github.com/txn2/query)
[![Docker Container Image Size](https://shields.beevelop.com/docker/image/image-size/txn2/query/latest.svg)](https://hub.docker.com/r/txn2/query/)
[![Docker Container Layers](https://shields.beevelop.com/docker/image/layers/txn2/rxtx/latest.svg)](https://hub.docker.com/r/txn2/irsync/)

WIP: query TXN2 data by account, model and index pattern. Save queries and execute saved queries.


## Examples

Start server:
```bash
go run ./cmd/query.go --tokenKey=$TOKEN_KEY --esServer=http://elasticsearch:9200
```

Run / Test a query:
```bash
curl -X POST \
  http://localhost:8080/run/xorg \
  -H 'Authorization: Bearer $TOKEN' \
  -d '{
    "machine_name": "all_los_angeles_parking_citations",
    "display_name": "All Los Angeles Parking Citations",
    "description_brief": "Gets all Los Angeles parking citation records available.",
    "description": "This is a dataset hosted by the city of Los Angeles. The organization has an open data platform found [here](https://data.lacity.org/)",
    "query_class": "table",
    "model": "los_angeles_parking_citations",
    "idx_pattern": "-testset",
    "query": {
	  "query": {
	    "match_all": {}
	  }
	}
}'
```

Upsert a query:
```bash
curl -X POST \
  http://localhost:8080/upsert/xorg \
  -H 'Authorization: Bearer $TOKEN' \
  -d '{
    "machine_name": "all_los_angeles_parking_citations",
    "display_name": "All Los Angeles Parking Citations",
    "description_brief": "Gets all Los Angeles parking citation records available",
    "description": "This is a dataset hosted by the city of Los Angeles. The organization has an open data platform found [here](https://data.lacity.org/)",
    "query_class": "table",
    "model": "los_angeles_parking_citations",
    "idx_pattern": "-testset",
    "query": {
	  "query": {
	    "match_all": {}
	  }
	}
}'
```

Search for queries:
```bash
curl -X POST \
  http://localhost:8080/search/xorg \
  -H 'Authorization: Bearer $TOKEN' \
  -d '{
  "size": 10,
  "query": {
    "match_all": {}
  }
}'
```

Get a query:
```bash
curl -X GET \
  http://localhost:8080/get/xorg/all_los_angeles_parking_citations \
  -H 'Authorization: Bearer $TOKEN'
```

## Release Packaging

Build test release:
```bash
goreleaser --skip-publish --rm-dist --skip-validate
```

Build and release:
```bash
GITHUB_TOKEN=$GITHUB_TOKEN goreleaser --rm-dist
```