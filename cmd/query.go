/*
   Copyright 2019 txn2
   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at
       http://www.apache.org/licenses/LICENSE-2.0
   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/
package main

import (
	"flag"
	"os"

	"github.com/txn2/micro"
	"github.com/txn2/provision"
	"github.com/txn2/query"
)

var (
	elasticServerEnv = getEnv("ELASTIC_SERVER", "http://elasticsearch:9200")
)

func main() {
	esServer := flag.String("esServer", elasticServerEnv, "Elasticsearch Server")

	serverCfg, _ := micro.NewServerCfg("Query")
	server := micro.NewServer(serverCfg)

	qApi, err := query.NewApi(&query.Config{
		Logger:        server.Logger,
		HttpClient:    server.Client,
		ElasticServer: *esServer,
	})
	if err != nil {
		server.Logger.Fatal("failure to instantiate the query API: " + err.Error())
		os.Exit(1)
	}

	// User token middleware
	server.Router.Use(provision.UserTokenHandler())

	// run a query (one-off operation for running or testing queries"
	server.Router.POST("run/:account",
		provision.AccountAccessCheckHandler(false),
		qApi.RunQueryHandler,
	)

	// Execute a query
	server.Router.GET("exec/:account/:id",
		provision.AccountAccessCheckHandler(false),
		qApi.ExecuteQueryHandler,
	)

	// Upsert a query
	server.Router.POST("upsert/:account",
		provision.AccountAccessCheckHandler(true),
		qApi.UpsertQueryHandler,
	)

	// Get a query
	server.Router.GET("get/:account/:id",
		provision.AccountAccessCheckHandler(false),
		qApi.GetQueryHandler,
	)

	// Search for queries
	server.Router.POST("search/:account",
		provision.AccountAccessCheckHandler(false),
		qApi.SearchQueryHandler,
	)

	// run provisioning server
	server.Run()
}

// getEnv gets an environment variable or sets a default if
// one does not exist.
func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}

	return value
}
