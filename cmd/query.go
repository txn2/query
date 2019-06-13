package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/patrickmn/go-cache"
	"github.com/txn2/ack"
	"github.com/txn2/micro"
	"github.com/txn2/provision"
	"github.com/txn2/query"
	"go.uber.org/zap"
)

var (
	// protected is intended for public use with BasicAuth and API Keys (use internal to bypass auth)
	modeEnv            = getEnv("MODE", "protected")
	elasticServerEnv   = getEnv("ELASTIC_SERVER", "http://elasticsearch:9200")
	provisionServerEnv = getEnv("PROVISION_SERVER", "http://provision:8070")
	authCacheEnv       = getEnv("AUTH_CACHE", "60")
	systemPrefixEnv    = getEnv("SYSTEM_PREFIX", "system_")
)

func main() {
	authCacheInt, err := strconv.Atoi(authCacheEnv)
	if err != nil {
		fmt.Println("Parsing error, AUTH_CACHE must be an integer in seconds.")
		os.Exit(1)
	}

	mode := flag.String("mode", modeEnv, "Protected or internal modes. (internal = security bypass)")
	esServer := flag.String("esServer", elasticServerEnv, "Elasticsearch Server")
	provisionServer := flag.String("provisionServer", provisionServerEnv, "Provision Server (txn2/provision)")
	authCache := flag.Int("authCache", authCacheInt, "Seconds to cache key (BasicAuth) authentication.")
	systemPrefix := flag.String("systemPrefix", systemPrefixEnv, "Prefix for system indices.")

	serverCfg, _ := micro.NewServerCfg("Query")
	server := micro.NewServer(serverCfg)

	qApi, err := query.NewApi(&query.Config{
		Logger:          server.Logger,
		HttpClient:      server.Client,
		ElasticServer:   *esServer,
		SystemIdxPrefix: *systemPrefix,
	})
	if err != nil {
		server.Logger.Fatal("failure to instantiate the query API: " + err.Error())
		os.Exit(1)
	}

	// setup authentication cache
	csh := cache.New(time.Duration(*authCache)*time.Second, 10*time.Minute)

	accessHandler := func(checkAdmin bool) gin.HandlerFunc {
		return func(c *gin.Context) {

			// check for basic auth first. if found, bypass token check
			// basic auth requires a network hit to provision, to
			// reduce latency in future calls cache results for a time
			name, key, ok := c.Request.BasicAuth()
			if ok {

				cacheKey := name + key
				// check cache
				cacheResult, found := csh.Get(cacheKey)
				if found {
					if cacheResult.(bool) {
						return
					}

					ak := ack.Gin(c)
					ak.SetPayload("Unauthorized via cache.")
					ak.GinErrorAbort(401, "E401", "UnauthorizedFailure")
					return
				}

				accessKey := provision.AccessKey{
					Name: name,
					Key:  key,
				}

				url := fmt.Sprintf("%s/keyCheck/%s", *provisionServer, c.Param("account"))
				server.Logger.Debug("Authenticating BasicAuth with AccessKey", zap.String("url", url))

				payload, _ := json.Marshal(accessKey)
				req, _ := http.NewRequest("POST", url, bytes.NewBuffer(payload))
				req.Header.Add("Content-Type", "application/json")

				res, err := server.Client.Http.Do(req)
				if err != nil {
					ak := ack.Gin(c)
					ak.SetPayload("Error contacting provision service.")
					ak.GinErrorAbort(500, "E500", err.Error())
					csh.Set(cacheKey, false, cache.DefaultExpiration)
					return
				}

				if res.StatusCode != 200 {
					ak := ack.Gin(c)
					ak.SetPayload("Unable to authenticate using BasicAuth.")
					ak.GinErrorAbort(res.StatusCode, "E"+strconv.Itoa(res.StatusCode), "AuthenticationFailure")
					csh.Set(cacheKey, false, cache.DefaultExpiration)
					return
				}

				csh.Set(cacheKey, true, cache.DefaultExpiration)
				return
			}

			// Load token
			tokenHandler := provision.UserTokenHandler()
			tokenHandler(c)

			if c.IsAborted() {
				return
			}

			// Check token
			tokenCheck := provision.AccountAccessCheckHandler(checkAdmin)
			tokenCheck(c)
		}
	}

	if *mode == "internal" {
		accessHandler = func(admin bool) gin.HandlerFunc {
			return func(c *gin.Context) {}
		}
	}

	// Run a query (one-off operation for running or testing queries
	server.Router.POST("run/:account",
		accessHandler(false),
		qApi.RunQueryHandler,
	)

	// Execute a stored query
	server.Router.GET("exec/:account/:id",
		accessHandler(false),
		qApi.ExecuteQueryHandlerF(false),
	)

	// Execute a system query for an account
	server.Router.GET("/system/exec/:account/:id",
		accessHandler(false),
		qApi.ExecuteQueryHandlerF(true),
	)

	// Upsert a query
	server.Router.POST("upsert/:account",
		accessHandler(true),
		qApi.UpsertQueryHandler,
	)

	// Get a query
	server.Router.GET("get/:account/:id",
		accessHandler(false),
		qApi.GetQueryHandler,
	)

	// Search for queries
	server.Router.POST("search/:account",
		accessHandler(false),
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
